package projects

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoredCaptureSelectsEveryCapturedSKUByDefault(t *testing.T) {
	pool := openIntegrationPool(t)
	ctx := context.Background()
	userID, projectID, extensionID, sessionID, sourceID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertCaptureFixture(t, pool, userID, projectID, extensionID, sessionID, sourceID, taskID, "dispatched")

	capture := CaptureResult{
		SourceURL: "https://item.jd.com/1.html",
		RootSKU:   "1001",
		Products: []CaptureProduct{
			{SKU: "1001", Title: "商品一", ResolvedURL: "https://item.jd.com/1001.html", VariantLabel: "款式一", SeriesLabel: "系列一", Summary: map[string]string{}, Parameters: map[string]string{}, Images: map[string][]string{}},
			{SKU: "1002", Title: "商品二", ResolvedURL: "https://item.jd.com/1002.html", VariantLabel: "款式二", SeriesLabel: "系列一", Summary: map[string]string{}, Parameters: map[string]string{}, Images: map[string][]string{}},
		},
	}
	if err := NewService(pool).StoreCapture(ctx, taskID, extensionID, capture); err != nil {
		t.Fatalf("store capture: %v", err)
	}

	var total, selected int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE selected) FROM project_sku_selections WHERE project_id=$1`, projectID).Scan(&total, &selected); err != nil {
		t.Fatalf("query selections: %v", err)
	}
	if total != 2 || selected != total {
		t.Fatalf("selected SKUs = %d/%d, want every captured SKU selected", selected, total)
	}
}

func TestSelectionIsRejectedWhileCaptureIsRunning(t *testing.T) {
	pool := openIntegrationPool(t)
	userID, projectID, extensionID, sessionID, sourceID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertCaptureFixture(t, pool, userID, projectID, extensionID, sessionID, sourceID, taskID, "queued")

	err := NewService(pool).UpdateSelection(context.Background(), projectID, userID, false, nil)
	if !errors.Is(err, ErrSelectionNotReady) {
		t.Fatalf("UpdateSelection() error = %v, want ErrSelectionNotReady", err)
	}
}

func TestRetryMarksOnlyFailedSourcesAsCollecting(t *testing.T) {
	pool := openIntegrationPool(t)
	ctx := context.Background()
	userID, projectID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role) VALUES($1,$2,$3,$4,'member')`, userID, userID.String()+"@example.test", []byte("test"), "retry status test"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO projects(id,owner_id,name,status,failure_code,failure_detail) VALUES($1,$2,'retry status test','failed','rate_limited','京东访问频率限制')`, projectID, userID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_sources(id,project_id,ordinal,source_url,status,failure_code,failure_detail) VALUES($1,$2,0,'https://item.jd.com/1.html','succeeded',NULL,NULL),($3,$2,1,'https://item.jd.com/2.html','failed','rate_limited','京东访问频率限制')`, uuid.New(), projectID, uuid.New()); err != nil {
		t.Fatalf("insert sources: %v", err)
	}

	if err := NewService(pool).Retry(ctx, projectID, userID, false); err != nil {
		t.Fatalf("Retry(): %v", err)
	}
	rows, err := pool.Query(ctx, `SELECT status,COALESCE(failure_code,'') FROM project_sources WHERE project_id=$1 ORDER BY ordinal`, projectID)
	if err != nil {
		t.Fatalf("query sources: %v", err)
	}
	defer rows.Close()
	var got [][2]string
	for rows.Next() {
		var status, code string
		if err := rows.Scan(&status, &code); err != nil {
			t.Fatalf("scan source: %v", err)
		}
		got = append(got, [2]string{status, code})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sources: %v", err)
	}
	if want := [][2]string{{"succeeded", ""}, {"collecting", ""}}; !slices.Equal(got, want) {
		t.Fatalf("source states = %#v, want %#v", got, want)
	}
}

func openIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertCaptureFixture(t *testing.T, pool *pgxpool.Pool, userID, projectID, extensionID, sessionID, sourceID, taskID uuid.UUID, taskStatus string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role) VALUES($1,$2,$3,$4,'member')`, userID, userID.String()+"@example.test", []byte("test"), "project integration test"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO projects(id,owner_id,name,status) VALUES($1,$2,'integration test','collecting')`, projectID, userID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id=$1`, projectID) })
	tokenHash := sha256.Sum256([]byte(uuid.NewString()))
	if _, err := pool.Exec(ctx, `INSERT INTO browser_extensions(id,user_id,device_name,token_hash) VALUES($1,$2,'test',$3)`, extensionID, userID, tokenHash[:]); err != nil {
		t.Fatalf("insert extension: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_sources(id,project_id,ordinal,source_url,status) VALUES($1,$2,0,'https://item.jd.com/1.html','queued')`, sourceID, projectID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO capture_sessions(id,project_id,extension_id,status) VALUES($1,$2,$3,'running')`, sessionID, projectID, extensionID); err != nil {
		t.Fatalf("insert capture session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO capture_tasks(id,capture_session_id,project_source_id,status) VALUES($1,$2,$3,$4)`, taskID, sessionID, sourceID, taskStatus); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if taskStatus == "dispatched" {
		if _, err := pool.Exec(ctx, `UPDATE project_sources SET status='collecting' WHERE id=$1`, sourceID); err != nil {
			t.Fatalf("mark source collecting: %v", err)
		}
	}
}
