package api

import (
	"github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/relayid"
)

func parseNodeIDOrUUID(value, expected string) (uuid.UUID, error) {
	id, err := relayid.DecodeUUID(value, expected)
	if err == nil {
		return uuid.UUID(id), nil
	}
	return uuid.FromString(value)
}
