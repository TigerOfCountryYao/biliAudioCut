package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordAndVerifyPassword(t *testing.T) {
	password := "correct-horse-battery-staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == password {
		t.Fatal("HashPassword() returned plaintext password")
	}

	if !VerifyPassword(hash, password) {
		t.Fatal("VerifyPassword() = false, want true")
	}

	if VerifyPassword(hash, "incorrect-password") {
		t.Fatal("VerifyPassword() = true for an incorrect password")
	}
}

func TestHashPasswordRejectsInvalidLength(t *testing.T) {
	if _, err := HashPassword("too-short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("HashPassword() error = %v, want ErrPasswordTooShort", err)
	}

	tooLong := strings.Repeat("a", maxPasswordBytes+1)
	if _, err := HashPassword(tooLong); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("HashPassword() error = %v, want ErrPasswordTooLong", err)
	}
}
