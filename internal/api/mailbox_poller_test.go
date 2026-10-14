package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"strings"
	"testing"
)

func TestForwardingTokenHashAcceptsLowercaseBase32(t *testing.T) {
	t.Parallel()
	token := make([]byte, sha256.Size)
	for i := range token {
		token[i] = byte(i)
	}
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	tag := strings.ToLower(encoding.EncodeToString(token))
	want := sha256.Sum256(token)

	got, ok := forwardingTokenHash("horse-mail+"+tag+"@walnuts.dev", "horse-mail@walnuts.dev")
	if !ok || !bytes.Equal(got, want[:]) {
		t.Fatalf("forwardingTokenHash() = %x, %t; want %x, true", got, ok, want)
	}
	if _, ok := forwardingTokenHash("horse-mail+"+strings.ToUpper(tag)+"@walnuts.dev", "horse-mail@walnuts.dev"); ok {
		t.Fatal("forwardingTokenHash() accepted an uppercase tag")
	}
}
