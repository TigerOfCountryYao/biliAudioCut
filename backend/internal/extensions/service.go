package extensions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUnauthorized = errors.New("extension unauthorized")
	ErrInvalidCode  = errors.New("invalid authorization code")
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Device struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DeviceName string
	Token      string
}

type DeviceStatus struct {
	Bound      bool   `json:"bound"`
	Connected  bool   `json:"connected"`
	DeviceName string `json:"device_name,omitempty"`
	BuildID    string `json:"build_id,omitempty"`
}
type Task struct {
	ID             uuid.UUID
	SourceID       uuid.UUID
	SourceURL      string
	CaptureAllSKUs bool
}

func randomToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(b), h[:], nil
}
func hashToken(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil, ErrUnauthorized
	}
	h := sha256.Sum256(b)
	return h[:], nil
}
func (s *Service) CreateAuthorizationCode(ctx context.Context, userID uuid.UUID, challenge, redirectURI string) (string, error) {
	if len(challenge) < 43 || len(challenge) > 128 || !strings.HasPrefix(redirectURI, "https://") || !strings.Contains(redirectURI, ".chromiumapp.org/") {
		return "", ErrInvalidCode
	}
	code, hash, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO extension_authorization_codes(user_id,code_hash,code_challenge,redirect_uri,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, userID, hash, challenge, redirectURI)
	return code, err
}
func (s *Service) ExchangeAuthorizationCode(ctx context.Context, code, verifier, deviceName string) (Device, error) {
	if len(deviceName) == 0 || len(deviceName) > 100 {
		return Device{}, ErrInvalidCode
	}
	hash, err := hashToken(code)
	if err != nil {
		return Device{}, ErrInvalidCode
	}
	v := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(v[:])
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	var stored string
	err = tx.QueryRow(ctx, `SELECT user_id,code_challenge FROM extension_authorization_codes WHERE code_hash=$1 AND consumed_at IS NULL AND expires_at>now() FOR UPDATE`, hash).Scan(&userID, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrInvalidCode
	}
	if err != nil {
		return Device{}, err
	}
	if stored != challenge {
		return Device{}, ErrInvalidCode
	}
	token, tokenHash, err := randomToken()
	if err != nil {
		return Device{}, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO browser_extensions(user_id,device_name,token_hash,revoked_at) VALUES($1,$2,$3,NULL) ON CONFLICT(user_id) DO UPDATE SET device_name=EXCLUDED.device_name,token_hash=EXCLUDED.token_hash,revoked_at=NULL,updated_at=now() RETURNING id`, userID, deviceName, tokenHash).Scan(&id)
	if err != nil {
		return Device{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE extension_authorization_codes SET consumed_at=now() WHERE code_hash=$1`, hash); err != nil {
		return Device{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Device{}, err
	}
	return Device{ID: id, UserID: userID, DeviceName: deviceName, Token: token}, nil
}
func (s *Service) Authenticate(ctx context.Context, raw string) (Device, error) {
	h, err := hashToken(raw)
	if err != nil {
		return Device{}, ErrUnauthorized
	}
	var d Device
	err = s.pool.QueryRow(ctx, `SELECT id,user_id,device_name FROM browser_extensions WHERE token_hash=$1 AND revoked_at IS NULL`, h).Scan(&d.ID, &d.UserID, &d.DeviceName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrUnauthorized
	}
	if err != nil {
		return Device{}, err
	}
	return d, nil
}
func (s *Service) Disconnect(ctx context.Context, raw string) error {
	h, err := hashToken(raw)
	if err != nil {
		return ErrUnauthorized
	}
	tag, err := s.pool.Exec(ctx, `UPDATE browser_extensions SET revoked_at=now(),updated_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, h)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) DeviceForToken(ctx context.Context, raw string) (Device, error) {
	return s.Authenticate(ctx, raw)
}
func (s *Service) Touch(ctx context.Context, id uuid.UUID, buildID string) error {
	buildID = strings.TrimSpace(buildID)
	if len(buildID) > 100 {
		buildID = ""
	}
	_, err := s.pool.Exec(ctx, `UPDATE browser_extensions SET connected_at=now(),last_seen_at=now(),build_id=COALESCE(NULLIF($2,''),build_id),updated_at=now() WHERE id=$1`, id, buildID)
	return err
}

func (s *Service) StatusForUser(ctx context.Context, userID uuid.UUID) (DeviceStatus, error) {
	var status DeviceStatus
	err := s.pool.QueryRow(ctx, `SELECT device_name,COALESCE(build_id,''),COALESCE(last_seen_at > now()-interval '2 minutes',false) FROM browser_extensions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&status.DeviceName, &status.BuildID, &status.Connected)
	if errors.Is(err, pgx.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return DeviceStatus{}, err
	}
	status.Bound = true
	return status, nil
}

func (s *Service) RequeueDispatchedTasks(ctx context.Context, extensionID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE project_sources ps SET status='queued',updated_at=now() FROM capture_tasks ct JOIN capture_sessions cs ON cs.id=ct.capture_session_id WHERE ct.project_source_id=ps.id AND cs.extension_id=$1 AND ct.status='dispatched'`, extensionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE capture_tasks ct SET status='queued',dispatched_at=NULL FROM capture_sessions cs WHERE ct.capture_session_id=cs.id AND cs.extension_id=$1 AND ct.status='dispatched'`, extensionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) StartCapture(ctx context.Context, projectID, userID uuid.UUID) ([]Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var extID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM browser_extensions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&extID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sessionID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO capture_sessions(project_id,extension_id,status) VALUES($1,$2,'running') RETURNING id`, projectID, extID).Scan(&sessionID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO capture_tasks(capture_session_id,project_source_id,status) SELECT $1,id,'queued' FROM project_sources WHERE project_id=$2 AND status IN ('queued','failed')`, sessionID, projectID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE projects SET status='collecting',updated_at=now() WHERE id=$1`, projectID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *Service) ClaimNextTask(ctx context.Context, extensionID uuid.UUID) (*Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var task Task
	err = tx.QueryRow(ctx, `WITH next_task AS (
		SELECT ct.id,ct.project_source_id,ps.source_url,p.capture_all_skus
		FROM capture_tasks ct
		JOIN capture_sessions cs ON cs.id=ct.capture_session_id
		JOIN project_sources ps ON ps.id=ct.project_source_id
		JOIN projects p ON p.id=cs.project_id
		WHERE cs.extension_id=$1 AND cs.status='running' AND ct.status='queued'
		ORDER BY cs.created_at,ps.ordinal,ct.id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	) UPDATE capture_tasks ct SET status='dispatched',dispatched_at=now() FROM next_task n WHERE ct.id=n.id RETURNING ct.id,n.source_url,n.project_source_id,n.capture_all_skus`, extensionID).Scan(&task.ID, &task.SourceURL, &task.SourceID, &task.CaptureAllSKUs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE project_sources SET status='collecting',updated_at=now() WHERE id=$1 AND status='queued'`, task.SourceID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &task, nil
}
func (s *Service) StartPendingCaptures(ctx context.Context, userID uuid.UUID) error {
	rows, err := s.pool.Query(ctx, `SELECT id FROM projects WHERE owner_id=$1 AND status='awaiting_extension' ORDER BY created_at`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.StartCapture(ctx, id, userID); err != nil {
			return err
		}
	}
	return nil
}

// FailTask records a source failure. It returns whether the device should
// immediately continue with the next source in the same capture session.
func (s *Service) FailTask(ctx context.Context, taskID, extID uuid.UUID, code, detail string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var projectID, sessionID, sourceID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT cs.project_id,cs.id,ct.project_source_id FROM capture_tasks ct JOIN capture_sessions cs ON cs.id=ct.capture_session_id WHERE ct.id=$1 AND cs.extension_id=$2 FOR UPDATE`, taskID, extID).Scan(&projectID, &sessionID, &sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUnauthorized
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE capture_tasks SET status='failed',failure_code=$2,failure_detail=$3,completed_at=now() WHERE id=$1`, taskID, code, detail); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE project_sources SET status='failed',failure_code=$2,failure_detail=$3,updated_at=now() WHERE id=$1`, sourceID, code, detail); err != nil {
		return false, err
	}
	blocking := code == "rate_limited" || code == "login_required" || code == "captcha_required"
	if blocking {
		if _, err = tx.Exec(ctx, `UPDATE capture_sessions SET status='failed',completed_at=now() WHERE id=$1 AND status='running'`, sessionID); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE projects SET status='failed',failure_code=$2,failure_detail=$3,updated_at=now() WHERE id=$1`, projectID, code, detail); err != nil {
			return false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}

	var remaining int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM capture_tasks WHERE capture_session_id=$1 AND status IN ('queued','dispatched')`, sessionID).Scan(&remaining); err != nil {
		return false, err
	}
	if remaining == 0 {
		if _, err = tx.Exec(ctx, `UPDATE capture_sessions SET status='failed',completed_at=now() WHERE id=$1 AND status='running'`, sessionID); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE projects SET status='failed',failure_code=COALESCE(failure_code,$2),failure_detail=COALESCE(failure_detail,$3),updated_at=now() WHERE id=$1`, projectID, code, detail); err != nil {
			return false, err
		}
	} else if _, err = tx.Exec(ctx, `UPDATE projects SET failure_code=COALESCE(failure_code,$2),failure_detail=COALESCE(failure_detail,$3),updated_at=now() WHERE id=$1`, projectID, code, detail); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return remaining > 0, nil
}
func (s *Service) String() string { return fmt.Sprint("extensions") }
