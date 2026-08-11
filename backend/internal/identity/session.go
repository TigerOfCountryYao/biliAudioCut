package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/identity/sqlc"
	"github.com/jackc/pgx/v5"
)

const (
	sessionTokenBytes = 32
	sessionLifetime   = 7 * 24 * time.Hour
)

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUnauthenticated     = errors.New("unauthenticated")
	errInvalidSessionToken = errors.New("invalid session token")
)

type LoginInput struct {
	Email    string
	Password string
}

type AuthenticatedSession struct {
	Token     string
	ExpiresAt time.Time
	User      identitysqlc.User
}

func (s *Service) Login(ctx context.Context, input LoginInput) (AuthenticatedSession, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return AuthenticatedSession{}, ErrInvalidCredentials
	}

	queries := identitysqlc.New(s.pool)

	user, err := queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthenticatedSession{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("get user by email: %w", err)
	}

	if user.Status != "active" || !VerifyPassword(user.PasswordHash, input.Password) {
		return AuthenticatedSession{}, ErrInvalidCredentials
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("generate session token: %w", err)
	}

	session, err := queries.CreateUserSession(ctx, identitysqlc.CreateUserSessionParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().UTC().Add(sessionLifetime),
	})
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("create user session: %w", err)
	}

	return AuthenticatedSession{
		Token:     token,
		ExpiresAt: session.ExpiresAt,
		User:      user,
	}, nil
}

func (s *Service) CurrentUser(ctx context.Context, token string) (identitysqlc.User, error) {
	tokenHash, err := hashSessionToken(token)
	if err != nil {
		return identitysqlc.User{}, ErrUnauthenticated
	}

	queries := identitysqlc.New(s.pool)

	session, err := queries.GetActiveSessionByTokenHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return identitysqlc.User{}, ErrUnauthenticated
	}
	if err != nil {
		return identitysqlc.User{}, fmt.Errorf("get active session: %w", err)
	}

	user, err := queries.GetUserByID(ctx, session.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identitysqlc.User{}, ErrUnauthenticated
	}
	if err != nil {
		return identitysqlc.User{}, fmt.Errorf("get session user: %w", err)
	}

	if user.Status != "active" {
		return identitysqlc.User{}, ErrUnauthenticated
	}

	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	tokenHash, err := hashSessionToken(token)
	if errors.Is(err, errInvalidSessionToken) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hash session token: %w", err)
	}

	queries := identitysqlc.New(s.pool)

	if _, err := queries.RevokeUserSessionByTokenHash(ctx, tokenHash); err != nil {
		return fmt.Errorf("revoke user session: %w", err)
	}

	return nil
}

func newSessionToken() (string, []byte, error) {
	tokenBytes := make([]byte, sessionTokenBytes)

	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, err
	}

	sum := sha256.Sum256(tokenBytes)

	return base64.RawURLEncoding.EncodeToString(tokenBytes), sum[:], nil
}

func hashSessionToken(token string) ([]byte, error) {
	tokenBytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(tokenBytes) != sessionTokenBytes {
		return nil, errInvalidSessionToken
	}

	sum := sha256.Sum256(tokenBytes)
	return sum[:], nil
}
