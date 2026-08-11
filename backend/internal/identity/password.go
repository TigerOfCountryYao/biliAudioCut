package identity

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordLength = 12
	maxPasswordBytes  = 72
	bcryptCost        = 12
)

var (
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 72 bytes")
)

func HashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return ErrPasswordTooShort
	}

	if len([]byte(password)) > maxPasswordBytes {
		return ErrPasswordTooLong
	}

	return nil
}
