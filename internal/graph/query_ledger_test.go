package graph

import (
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

func TestLedgerTotalCountRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		requested bool
	}{
		{
			name:  "omitted",
			query: `query { ledger { edges { cursor } } }`,
		},
		{
			name:      "aliased through fragment",
			query:     `query { ledger { ...LedgerCount } } fragment LedgerCount on LedgerConnection { count: totalCount }`,
			requested: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			doc, err := parser.ParseQuery(&ast.Source{Input: test.query})
			if err != nil {
				t.Fatal(err)
			}
			ledgerField := doc.Operations[0].SelectionSet[0].(*ast.Field)
			ctx := graphql.WithOperationContext(t.Context(), &graphql.OperationContext{Doc: doc, Variables: map[string]any{}})
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{Field: graphql.CollectedField{Field: ledgerField, Selections: ledgerField.SelectionSet}})

			if requested := ledgerTotalCountRequested(ctx); requested != test.requested {
				t.Fatalf("totalCount requested = %t, want %t", requested, test.requested)
			}
		})
	}
}
