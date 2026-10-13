package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
)

func NodeIDDirective(ctx context.Context, value any, next graphql.Resolver, of string) (any, error) {
	resolved, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}

	switch globalID := resolved.(type) {
	case string:
		return decodeNodeID(globalID, of)
	case []string:
		decoded := make([]string, len(globalID))
		for i := range globalID {
			id, err := decodeNodeID(globalID[i], of)
			if err != nil {
				return nil, err
			}
			decoded[i] = id
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("node ID must be a string or a list of strings")
	}
}
