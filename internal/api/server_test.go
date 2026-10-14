package api

import (
	"bytes"
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
	ctx := graphql.WithFieldContext(t.Context(), field)
	server.logGraphQLInputRejection(ctx, errors.New("private cursor value"))

	log := output.String()
	if !strings.Contains(log, "graphql_path=purchaseChanges") {
		t.Errorf("log does not include the resolver path: %s", log)
	}
	if strings.Contains(log, "private cursor value") {
		t.Errorf("log includes rejected input: %s", log)
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
