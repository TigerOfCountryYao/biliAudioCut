package identity

import (
	"bytes"
	"testing"
)

func TestNewSessionTokenCanBeHashedAgain(t *testing.T) {
	token, generatedHash, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}

	if len(generatedHash) != 32 {
		t.Fatalf("hash length = %d, want 32", len(generatedHash))
	}

	recomputedHash, err := hashSessionToken(token)
	if err != nil {
		t.Fatalf("hashSessionToken() error = %v", err)
	}

	if !bytes.Equal(generatedHash, recomputedHash) {
		t.Fatal("session token hashes differ")
	}
}

func TestHashSessionTokenRejectsInvalidToken(t *testing.T) {
	if _, err := hashSessionToken("not-a-valid-session-token"); err == nil {
		t.Fatal("hashSessionToken() error = nil, want an error")
	}
}
