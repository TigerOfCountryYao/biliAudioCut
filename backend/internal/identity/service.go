package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/identity/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const bootstrapLockKey int64 = 901327

var (
	ErrAlreadyInitialized = errors.New("system already has users")
	ErrEmailAlreadyExists = errors.New("email is already in use")
	ErrInvalidEmail       = errors.New("email is invalid")
	ErrInvalidDisplayName = errors.New("display name is required")
)

type Service struct {
	pool *pgxpool.Pool
}

type BootstrapAdminInput struct {
	Email       string
	DisplayName string
	Password    string
}

type CreateMemberInput struct {
	Email       string
	DisplayName string
	Password    string
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		pool: pool,
	}
}

func (s *Service) BootstrapAdmin(
	ctx context.Context,
	input BootstrapAdminInput,
) (identitysqlc.User, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return identitysqlc.User{}, err
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return identitysqlc.User{}, ErrInvalidDisplayName
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identitysqlc.User{}, fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapLockKey); err != nil {
		return identitysqlc.User{}, fmt.Errorf("lock bootstrap: %w", err)
	}

	queries := identitysqlc.New(tx)

	hasUsers, err := queries.HasUsers(ctx)
	if err != nil {
		return identitysqlc.User{}, fmt.Errorf("check existing users: %w", err)
	}
	if hasUsers {
		return identitysqlc.User{}, ErrAlreadyInitialized
	}

	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return identitysqlc.User{}, err
	}

	user, err := queries.CreateUser(ctx, identitysqlc.CreateUserParams{
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Role:         "admin",
	})
	if err != nil {
		return identitysqlc.User{}, fmt.Errorf("create administrator: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return identitysqlc.User{}, fmt.Errorf("commit bootstrap transaction: %w", err)
	}

	return user, nil
}

// CreateMember creates an active member account. It is intended for trusted
// administrators running the local admin CLI, not for public registration.
func (s *Service) CreateMember(ctx context.Context, input CreateMemberInput) (identitysqlc.User, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return identitysqlc.User{}, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return identitysqlc.User{}, ErrInvalidDisplayName
	}
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return identitysqlc.User{}, err
	}

	user, err := identitysqlc.New(s.pool).CreateUser(ctx, identitysqlc.CreateUserParams{
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Role:         "member",
	})
	if isUniqueViolation(err) {
		return identitysqlc.User{}, ErrEmailAlreadyExists
	}
	if err != nil {
		return identitysqlc.User{}, fmt.Errorf("create member: %w", err)
	}
	return user, nil
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", ErrInvalidEmail
	}

	return email, nil
}
