package relayid

import (
	"bytes"
	"encoding/base64"
	"fmt"
)

func Encode(kind string, payload []byte) string {
	encoded := make([]byte, 0, len(kind)+1+len(payload))
	encoded = append(encoded, kind...)
	encoded = append(encoded, ':')
	encoded = append(encoded, payload...)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func Decode(globalID string) (string, []byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(globalID)
	if err != nil {
		return "", nil, fmt.Errorf("invalid node ID: %w", err)
	}
	kind, payload, ok := bytes.Cut(decoded, []byte{':'})
	if !ok {
		return "", nil, fmt.Errorf("invalid node ID payload")
	}
	return string(kind), payload, nil
}

func DecodeUUID(globalID, expected string) ([16]byte, error) {
	kind, payload, err := Decode(globalID)
	if err != nil {
		return [16]byte{}, err
	}
	if kind != expected {
		return [16]byte{}, fmt.Errorf("expected %s node ID, got %s", expected, kind)
	}
	if len(payload) != 16 {
		return [16]byte{}, fmt.Errorf("invalid %s node ID payload", expected)
	}
	var id [16]byte
	copy(id[:], payload)
	if id == [16]byte{} {
		return [16]byte{}, fmt.Errorf("invalid %s node ID", expected)
	}
	return id, nil
}
