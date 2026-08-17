package extensions

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClaimNextTaskFollowsProjectSourceOrdinal(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer pool.Close()

	userID := uuid.New()
	projectID := uuid.New()
	extensionID := uuid.New()
	sessionID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role) VALUES($1,$2,$3,$4,'member')`, userID, userID.String()+"@example.test", []byte("test"), "capture order test"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID) }()
	if _, err := pool.Exec(ctx, `INSERT INTO projects(id,owner_id,name,status) VALUES($1,$2,'capture order test','collecting')`, projectID, userID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, projectID) }()
	tokenHash := sha256.Sum256([]byte(uuid.NewString()))
	if _, err := pool.Exec(ctx, `INSERT INTO browser_extensions(id,user_id,device_name,token_hash) VALUES($1,$2,'test',$3)`, extensionID, userID, tokenHash[:]); err != nil {
		t.Fatalf("insert extension: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO capture_sessions(id,project_id,extension_id,status) VALUES($1,$2,$3,'running')`, sessionID, projectID, extensionID); err != nil {
		t.Fatalf("insert capture session: %v", err)
	}

	sourceIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	createdAt := time.Now().UTC()
	for ordinal, sourceID := range sourceIDs {
		if _, err := pool.Exec(ctx, `INSERT INTO project_sources(id,project_id,ordinal,source_url,status,created_at,updated_at) VALUES($1,$2,$3,$4,'queued',$5,$5)`, sourceID, projectID, ordinal, "https://item.jd.com/"+uuid.NewString()+".html", createdAt); err != nil {
			t.Fatalf("insert source %d: %v", ordinal, err)
		}
	}
	for _, sourceIndex := range []int{0, 2, 1} {
		if _, err := pool.Exec(ctx, `INSERT INTO capture_tasks(id,capture_session_id,project_source_id,status,created_at) VALUES($1,$2,$3,'queued',$4)`, uuid.New(), sessionID, sourceIDs[sourceIndex], createdAt); err != nil {
			t.Fatalf("insert task %d: %v", sourceIndex, err)
		}
	}

	service := NewService(pool)
	for ordinal, wantSourceID := range sourceIDs {
		task, err := service.ClaimNextTask(ctx, extensionID)
		if err != nil {
			t.Fatalf("claim task %d: %v", ordinal, err)
		}
		if task == nil || task.SourceID != wantSourceID {
			t.Fatalf("claim task %d source = %v, want %v", ordinal, task, wantSourceID)
		}
	}
}

func TestClaimNextTaskIncludesProjectCaptureScope(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer pool.Close()

	userID, projectID, extensionID, sessionID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role) VALUES($1,$2,$3,$4,'member')`, userID, userID.String()+"@example.test", []byte("test"), "capture scope test"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID) }()
	if _, err := pool.Exec(ctx, `INSERT INTO projects(id,owner_id,name,status,capture_all_skus) VALUES($1,$2,'capture scope test','collecting',true)`, projectID, userID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, projectID) }()
	tokenHash := sha256.Sum256([]byte(uuid.NewString()))
	if _, err := pool.Exec(ctx, `INSERT INTO browser_extensions(id,user_id,device_name,token_hash) VALUES($1,$2,'test',$3)`, extensionID, userID, tokenHash[:]); err != nil {
		t.Fatalf("insert extension: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO capture_sessions(id,project_id,extension_id,status) VALUES($1,$2,$3,'running')`, sessionID, projectID, extensionID); err != nil {
		t.Fatalf("insert capture session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_sources(id,project_id,ordinal,source_url,status) VALUES($1,$2,0,'https://item.jd.com/1.html','queued')`, sourceID, projectID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO capture_tasks(capture_session_id,project_source_id,status) VALUES($1,$2,'queued')`, sessionID, sourceID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	task, err := NewService(pool).ClaimNextTask(ctx, extensionID)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if task == nil || !task.CaptureAllSKUs {
		t.Fatalf("claimed task capture_all_skus = %v, want true", task)
	}
}

func TestReplacingExtensionTokenInvalidatesThePreviousToken(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer pool.Close()

	userID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role) VALUES($1,$2,$3,$4,'member')`, userID, userID.String()+"@example.test", []byte("test"), "device replacement test"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID) }()

	service := NewService(pool)
	firstRaw, firstHash, err := randomToken()
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	var deviceID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO browser_extensions(user_id,device_name,token_hash) VALUES($1,'first device',$2) RETURNING id`, userID, firstHash).Scan(&deviceID); err != nil {
		t.Fatalf("insert extension: %v", err)
	}
	secondRaw, secondHash, err := randomToken()
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE browser_extensions SET device_name='second device',token_hash=$1,updated_at=now() WHERE id=$2`, secondHash, deviceID); err != nil {
		t.Fatalf("replace extension token: %v", err)
	}
	if _, err := service.Authenticate(ctx, firstRaw); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old token authentication error = %v, want ErrUnauthorized", err)
	}
	device, err := service.Authenticate(ctx, secondRaw)
	if err != nil {
		t.Fatalf("new token authentication error: %v", err)
	}
	if device.ID != deviceID || device.UserID != userID {
		t.Fatalf("authenticated device = %+v, want id %s and user %s", device, deviceID, userID)
	}
}
