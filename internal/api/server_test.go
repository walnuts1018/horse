package api

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vektah/gqlparser/v2/ast"
)

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

func TestLogGraphQLResolverFailureSkipsErrorsWithoutFieldPath(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	server := New(nil, nil, slog.New(slog.NewTextHandler(&output, nil)), Config{})
	server.logGraphQLResolverFailure(t.Context(), errors.New("private resolver error"))
	if output.Len() != 0 {
		t.Fatalf("unexpected log without GraphQL field path: %s", output.String())
	}
}
