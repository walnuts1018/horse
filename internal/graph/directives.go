package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
)

func NodeIDDirective(ctx context.Context, value any, next graphql.Resolver, of string) (any, error) {
	if value == nil {
		return nil, nil
	}
	globalID, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("node ID must be a string")
	}
	id, err := decodeNodeID(globalID, of)
	if err != nil {
		return nil, err
	}
	return id, nil
}
