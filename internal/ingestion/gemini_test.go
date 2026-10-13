package ingestion

import (
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testGeminiModel = "test-model"

func TestReceiptSchemaMatchesJSONTypesAndRequiredFields(t *testing.T) {
	t.Parallel()
	ignoredFields := map[reflect.Type]map[string]bool{
		reflect.TypeFor[Receipt](): {"Model": true, "PromptVersion": true, "InputTokens": true, "OutputTokens": true, "Statements": true},
		reflect.TypeFor[Item]():    {"GrossAmount": true, "Verification": true},
	}
	optionalFields := map[reflect.Type]map[string]bool{
		reflect.TypeFor[Receipt](): {"OrderID": true, "TransactionID": true},
		reflect.TypeFor[Item]():    {"CategoryHint": true},
	}
	assertSchemaMatchesType(t, reflect.TypeFor[Receipt](), receiptSchema(), ignoredFields, optionalFields)
}

func TestGeminiExtractsSourceKindSpecificStatementRows(t *testing.T) {
	t.Parallel()
	region := `{"page":0,"top":0.1,"left":0.1,"bottom":0.2,"right":0.9}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGeminiSuccess(w, `{"transactions":[{"kind":"CARD_PURCHASE","occurredAt":"2026-10-12T12:00:00+09:00","merchant":"Market","amount":-1234,"transactionId":"tx-1","orderId":"","cardLast4":"1234","region":`+region+`}],"warnings":[]}`)
	}))
	defer server.Close()
	gemini := Gemini{APIKey: testGeminiAPIKey, Model: testGeminiModel, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	receipt, _, err := gemini.ExtractRun(t.Context(), Document{MIMEType: mimeTypePDF, Data: []byte("synthetic pdf bytes")}, nil, GeminiRequestOptions{SourceKind: statementSourceCard})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyStatements(statementSourceCard, receipt.Statements, receipt.Warnings)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Statements) != 1 || verified.Statements[0].Amount != -1234 || verified.Statements[0].SourceReference != "row:1" || verified.Statements[0].Region.Page != 0 {
		t.Fatalf("statement extraction lost signed amount or source geometry: %+v", verified.Statements)
	}
}

func assertSchemaMatchesType(t *testing.T, typ reflect.Type, schema map[string]any, ignoredFields, optionalFields map[reflect.Type]map[string]bool) {
	t.Helper()
	if schema[schemaTypeKey] != "object" {
		t.Fatalf("schema for %s has type %v, want object", typ, schema[schemaTypeKey])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema for %s has no properties", typ)
	}
	requiredValues, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema for %s has invalid required fields: %T", typ, schema["required"])
	}
	required := make(map[string]bool, len(requiredValues))
	for _, name := range requiredValues {
		required[name] = true
	}
	wantProperties := make(map[string]bool, typ.NumField())
	wantRequired := make(map[string]bool, len(requiredValues))
	for field := range typ.Fields() {
		if ignoredFields[typ][field.Name] {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		wantProperties[name] = true
		if !strings.Contains(","+options+",", ",omitempty,") && !optionalFields[typ][field.Name] {
			wantRequired[name] = true
		}
		property, exists := properties[name]
		if !exists {
			t.Errorf("schema for %s is missing property %q", typ, name)
			continue
		}
		assertSchemaFieldType(t, field.Type, property, ignoredFields, optionalFields)
	}
	for name := range properties {
		if !wantProperties[name] {
			t.Errorf("schema for %s has unexpected property %q", typ, name)
		}
	}
	for name := range wantRequired {
		if !required[name] {
			t.Errorf("schema for %s does not require %q", typ, name)
		}
	}
	for name := range required {
		if !wantRequired[name] {
			t.Errorf("schema for %s unexpectedly requires %q", typ, name)
		}
	}
}

func assertSchemaFieldType(t *testing.T, typ reflect.Type, schemaValue any, ignoredFields, optionalFields map[reflect.Type]map[string]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	schema, ok := schemaValue.(map[string]any)
	if !ok {
		t.Fatalf("schema for %s is not an object", typ)
	}
	wantType := ""
	switch typ.Kind() {
	case reflect.String:
		wantType = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		wantType = schemaTypeInteger
	case reflect.Float32, reflect.Float64:
		wantType = schemaTypeNumber
	case reflect.Struct:
		assertSchemaMatchesType(t, typ, schema, ignoredFields, optionalFields)
		return
	case reflect.Slice, reflect.Array:
		wantType = "array"
		items, ok := schema[schemaItemsKey]
		if !ok {
			t.Fatalf("schema for %s has no array items", typ)
		}
		assertSchemaFieldType(t, typ.Elem(), items, ignoredFields, optionalFields)
	case reflect.Invalid, reflect.Bool, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.UnsafePointer:
		t.Fatalf("unsupported JSON schema type %s", typ)
	}
	if got := schema[schemaTypeKey]; got != wantType {
		t.Errorf("schema for %s has type %v, want %s", typ, got, wantType)
	}
}

func TestGeminiExtractPreservesExactQuantityAndPrintedUnitPrice(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/models/"+testGeminiModel+":generateContent" {
			t.Errorf("unexpected SDK request target: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != testGeminiAPIKey {
			t.Errorf("SDK request did not use the configured API key")
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read generation request: %v", err)
		}
		var request struct {
			ServiceTier string `json:"serviceTier"`
			Contents    []struct {
				Parts []struct {
					InlineData struct {
						Data     string `json:"data"`
						MIMEType string `json:"mimeType"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				ResponseMIMEType string `json:"responseMimeType"`
				MaxOutputTokens  int    `json:"maxOutputTokens"`
				ThinkingConfig   struct {
					ThinkingLevel string `json:"thinkingLevel"`
				} `json:"thinkingConfig"`
			} `json:"generationConfig"`
		}
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Errorf("decode SDK request: %v", err)
		}
		if request.ServiceTier != geminiServiceTierStandard || request.GenerationConfig.ResponseMIMEType != "application/json" || request.GenerationConfig.MaxOutputTokens != MaxOutputTokens || request.GenerationConfig.ThinkingConfig.ThinkingLevel != geminiThinkingLevelLOW {
			t.Errorf("SDK request omitted generation settings: %#v", request)
		}
		if len(request.Contents) != 1 || len(request.Contents[0].Parts) != 2 || request.Contents[0].Parts[1].InlineData.MIMEType != mimeTypeJPEG || request.Contents[0].Parts[1].InlineData.Data != base64.StdEncoding.EncodeToString([]byte("synthetic image bytes")) {
			t.Errorf("SDK request changed inline image content: %#v", request.Contents)
		}
		for _, required := range []string{"quantityNumerator", "printedUnitPriceQuantityDenominator", "Never derive printed unit price from line total"} {
			if !strings.Contains(string(requestBody), required) {
				t.Errorf("generation request is missing quantity extraction guidance %q", required)
			}
		}
		writeGeminiSuccess(w, `{"merchant":"Market","purchasedAt":"2026-10-12T12:00:00+09:00","currency":"JPY","items":[{"printedName":"Flour","name":"Flour","printedAmount":240,"discount":0,"priceBasis":"TAX_INCLUDED","taxGroup":"","quantityNumerator":3,"quantityDenominator":2,"quantityUnit":"kg","printedUnitPriceYen":130,"printedUnitPriceQuantityNumerator":1,"printedUnitPriceQuantityDenominator":1,"printedUnitPriceUnit":"kg","confidence":0.99}],"taxGroups":[],"discount":0,"total":240,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()

	gemini := Gemini{APIKey: testGeminiAPIKey, Model: testGeminiModel, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	receipt, _, err := gemini.ExtractRun(t.Context(), Document{MIMEType: mimeTypeJPEG, Data: []byte("synthetic image bytes")}, nil, GeminiRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item := receipt.Items[0]
	if item.QuantityNumerator == nil || *item.QuantityNumerator != 3 || item.QuantityDenominator == nil || *item.QuantityDenominator != 2 || item.QuantityUnit != "kg" ||
		item.PrintedUnitPriceYen == nil || *item.PrintedUnitPriceYen != 130 || item.PrintedUnitPriceUnit != "kg" {
		t.Fatalf("receipt extraction lost rational quantity or printed unit price: %#v", item)
	}
}
