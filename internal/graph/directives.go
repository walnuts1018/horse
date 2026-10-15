package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
)

func NodeIDDirective(ctx context.Context, value any, next graphql.Resolver, of string) (any, error) {
	if _, ok := nodeTypes[of]; !ok {
		return nil, fmt.Errorf("unknown node type %q", of)
	}

	resolved, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}

	switch globalID := resolved.(type) {
	case string:
		return decodeDirectiveNodeID(globalID, of)
	case []string:
		decoded := make([]string, len(globalID))
		for i := range globalID {
			id, err := decodeDirectiveNodeID(globalID[i], of)
			if err != nil {
				return nil, err
			}
			decoded[i] = id
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("%w: node ID must be a string or a list of strings", ErrBadInput)
	}
}

func decodeDirectiveNodeID(globalID, of string) (string, error) {
	id, err := decodeNodeID(globalID, of)
	if err != nil {
		return "", fmt.Errorf("%w: invalid node ID", ErrBadInput)
	}

	return id, nil
}
