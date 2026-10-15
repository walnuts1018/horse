package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/walnuts1018/horse/internal/auth"
)

func TestHandlerPreservesGetRoutesForHeadRequests(t *testing.T) {
	t.Parallel()
	server := New(nil, nil, slog.New(slog.DiscardHandler), Config{})
	request := httptest.NewRequest(http.MethodHead, "/healthz", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("HEAD /healthz status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestGraphQLProtocolErrorsKeepTheirCodeAndHTTPStatus(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		query string
		code  string
	}{
		{name: "parse", query: "query {", code: "GRAPHQL_PARSE_FAILED"},
		{name: "validation", query: "query LedgerProbe { notAField }", code: "GRAPHQL_VALIDATION_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
			requestBody := fmt.Sprintf(`{"query":%q}`, test.query)
			request := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.graphqlHandler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("protocol error status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("protocol error code %q is missing: %s", test.code, response.Body.String())
			}
			if !strings.Contains(output.String(), "graphql_error_code="+test.code) {
				t.Fatalf("protocol error code %q is missing from safe diagnostics: %s", test.code, output.String())
			}
			if strings.Contains(output.String(), test.query) {
				t.Fatalf("protocol diagnostics exposed the query: %s", output.String())
			}
		})
	}
}

func TestAuthenticationFailuresSeparateMissingSessionFromLookupFailure(t *testing.T) {
	t.Parallel()
	server := New(nil, nil, slog.New(slog.DiscardHandler), Config{})
	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "missing session", err: auth.ErrUnauthenticated, wantStatus: http.StatusUnauthorized},
		{name: "lookup timeout", err: context.DeadlineExceeded, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/documents", nil)
			response := httptest.NewRecorder()
			server.writeAuthenticationFailure(response, request, test.err)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.Contains(response.Body.String(), "context deadline exceeded") {
				t.Fatalf("response exposed the authentication lookup cause: %s", response.Body.String())
			}
		})
	}
}

func TestAuthenticationFailureLogsRootCauseWithoutDatabaseMessage(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	request := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	server.writeAuthenticationUnavailable(httptest.NewRecorder(), request, fmt.Errorf("resolve OIDC session: %w", &pgconn.PgError{
		Code:    "40001",
		Message: "private database detail",
	}))
	log := output.String()
	if !strings.Contains(log, "error_type=*pgconn.PgError") || !strings.Contains(log, "database_code=40001") {
		t.Fatalf("authentication failure log is missing safe diagnostics: %s", log)
	}
	if strings.Contains(log, "private database detail") {
		t.Fatalf("authentication failure log exposed the database error message: %s", log)
	}
}

func TestAccessLogSkipsHealthProbes(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	handler := server.accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/healthz", "/readyz", "/documents"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
		}
	}

	log := output.String()
	if strings.Contains(log, "/healthz") || strings.Contains(log, "/readyz") {
		t.Errorf("health probes were logged: %s", log)
	}
	if !strings.Contains(log, "path=/documents") {
		t.Errorf("ordinary request was not logged: %s", log)
	}
}

func TestLogGraphQLResolverFailureIncludesSafePostgresMetadata(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	field := &graphql.FieldContext{Field: graphql.CollectedField{Field: &ast.Field{Name: "postPurchase", Alias: "clientChosenAlias"}}}
	ctx := graphql.WithFieldContext(t.Context(), field)
	pgError := &pgconn.PgError{
		Code:           "23503",
		SchemaName:     "public",
		TableName:      "purchase_payments",
		ColumnName:     "account_id",
		ConstraintName: "purchase_payments_household_id_account_id_fkey",
		Message:        "private value in rejected row",
		Detail:         "private detail",
		Hint:           "private hint",
		InternalQuery:  "SELECT private_sql",
	}
	server.logGraphQLResolverFailure(ctx, fmt.Errorf("resolver failure: %w", pgError))

	log := output.String()
	for _, safeField := range []string{
		"graphql_path=postPurchase",
		"postgres_code=23503",
		"postgres_schema=public",
		"postgres_table=purchase_payments",
		"postgres_column=account_id",
		"postgres_constraint=purchase_payments_household_id_account_id_fkey",
	} {
		if !strings.Contains(log, safeField) {
			t.Errorf("log does not contain %q: %s", safeField, log)
		}
	}
	for _, privateField := range []string{
		"clientChosenAlias",
		"private value",
		"private detail",
		"private hint",
		"private_sql",
		"SELECT",
		"resolver failure",
	} {
		if strings.Contains(log, privateField) {
			t.Errorf("log contains private field %q: %s", privateField, log)
		}
	}
}

func TestLogGraphQLInputRejectionOmitsErrorMessage(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	field := &graphql.FieldContext{Field: graphql.CollectedField{Field: &ast.Field{Name: "purchaseChanges"}}}
	operation := &graphql.OperationContext{Operation: &ast.OperationDefinition{Name: "PurchaseProbe"}}
	ctx := graphql.WithOperationContext(t.Context(), operation)
	ctx = graphql.WithFieldContext(ctx, field)
	server.logGraphQLInputRejection(ctx, errors.New("private cursor value"))

	log := output.String()
	if !strings.Contains(log, "graphql_path=purchaseChanges") {
		t.Errorf("log does not include the resolver path: %s", log)
	}
	if !strings.Contains(log, "operation=PurchaseProbe") {
		t.Errorf("log does not include the operation name: %s", log)
	}
	if strings.Contains(log, "private cursor value") {
		t.Errorf("log includes rejected input: %s", log)
	}
}

func TestLogGraphQLInputRejectionOmitsLongOperationName(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	operationName := strings.Repeat("a", maxGraphQLOperationNameLength+1)
	operation := &graphql.OperationContext{Operation: &ast.OperationDefinition{Name: operationName}}
	ctx := graphql.WithOperationContext(t.Context(), operation)
	server.logGraphQLInputRejection(ctx, errors.New("private cursor value"))
	if strings.Contains(output.String(), operationName) {
		t.Fatalf("log contains an overlong operation name: %s", output.String())
	}
}

func TestLogGraphQLResolverFailureSkipsErrorsWithoutFieldPath(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	server.logGraphQLResolverFailure(t.Context(), errors.New("private resolver error"))
	if output.Len() != 0 {
		t.Fatalf("unexpected log without GraphQL field path: %s", output.String())
	}
}
