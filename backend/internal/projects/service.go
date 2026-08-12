package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxLinksPerProject = 20

var (
	ErrNotFound      = errors.New("project not found")
	ErrInvalidInput  = errors.New("invalid input")
	ErrCaptureFailed = errors.New("capture failed")
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Project struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	FailureCode   *string   `json:"failure_code,omitempty"`
	FailureDetail *string   `json:"failure_detail,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Source struct {
	ID            uuid.UUID `json:"id"`
	Ordinal       int       `json:"ordinal"`
	SourceURL     string    `json:"source_url"`
	ResolvedURL   *string   `json:"resolved_url,omitempty"`
	Status        string    `json:"status"`
	FailureCode   *string   `json:"failure_code,omitempty"`
	FailureDetail *string   `json:"failure_detail,omitempty"`
	Products      []Product `json:"products"`
}

type Product struct {
	SnapshotID uuid.UUID `json:"snapshot_id"`
	RootSKU    string    `json:"root_sku"`
	Title      string    `json:"title"`
	SKUs       []SKU     `json:"skus"`
}

type SKU struct {
	ID          uuid.UUID `json:"id"`
	SKU         string    `json:"sku"`
	Title       string    `json:"title"`
	ResolvedURL string    `json:"resolved_url"`
	Price       *string   `json:"price,omitempty"`
	Selected    bool      `json:"selected"`
}

type Detail struct {
	Project Project  `json:"project"`
	Sources []Source `json:"sources"`
}

func normalizeLinks(links []string) ([]string, error) {
	unique := make([]string, 0, len(links))
	seen := map[string]bool{}
	for _, raw := range links {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || (parsed.Hostname() != "u.jd.com" && parsed.Hostname() != "item.jd.com") {
			return nil, fmt.Errorf("%w: link must be a JD item or short link", ErrInvalidInput)
		}
		if !seen[value] {
			unique, seen[value] = append(unique, value), true
		}
	}
	if len(unique) == 0 || len(unique) > maxLinksPerProject {
		return nil, fmt.Errorf("%w: provide 1-%d links", ErrInvalidInput, maxLinksPerProject)
	}
	return unique, nil
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name string, links []string) (Project, error) {
	links, err := normalizeLinks(links)
	if err != nil {
		return Project{}, err
	}
	name = strings.TrimSpace(name)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)
	var project Project
	err = tx.QueryRow(ctx, `INSERT INTO projects(owner_id,name,status) VALUES($1,NULLIF($2,''),'awaiting_extension') RETURNING id,COALESCE(name,''),status,failure_code,failure_detail,created_at,updated_at`, ownerID, name).Scan(&project.ID, &project.Name, &project.Status, &project.FailureCode, &project.FailureDetail, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	for ordinal, link := range links {
		if _, err := tx.Exec(ctx, `INSERT INTO project_sources(project_id,ordinal,source_url,status) VALUES($1,$2,$3,'queued')`, project.ID, ordinal, link); err != nil {
			return Project{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID, isAdmin bool) ([]Project, error) {
	query := `SELECT id,COALESCE(name,''),status,failure_code,failure_detail,created_at,updated_at FROM projects WHERE owner_id=$1 OR $2 ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query, ownerID, isAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Status, &p.FailureCode, &p.FailureDetail, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id, ownerID uuid.UUID, isAdmin bool) (Detail, error) {
	var d Detail
	err := s.pool.QueryRow(ctx, `SELECT id,COALESCE(name,''),status,failure_code,failure_detail,created_at,updated_at FROM projects WHERE id=$1 AND (owner_id=$2 OR $3)`, id, ownerID, isAdmin).Scan(&d.Project.ID, &d.Project.Name, &d.Project.Status, &d.Project.FailureCode, &d.Project.FailureDetail, &d.Project.CreatedAt, &d.Project.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT ps.id,ps.ordinal,ps.source_url,ps.resolved_url,ps.status,ps.failure_code,ps.failure_detail, p.id,p.root_sku,ss.id,ss.sku,ss.title,ss.resolved_url,ss.price,COALESCE(sel.selected,false)
FROM project_sources ps LEFT JOIN product_snapshots p ON p.project_source_id=ps.id LEFT JOIN snapshot_skus ss ON ss.snapshot_id=p.id LEFT JOIN project_sku_selections sel ON sel.project_id=ps.project_id AND sel.snapshot_sku_id=ss.id WHERE ps.project_id=$1 ORDER BY ps.ordinal,ss.ordinal`, id)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	sources := map[uuid.UUID]*Source{}
	products := map[uuid.UUID]*Product{}
	for rows.Next() {
		var source Source
		var productID *uuid.UUID
		var root *string
		var skuID *uuid.UUID
		var sku, skuTitle, skuURL, price *string
		var selected *bool
		if err := rows.Scan(&source.ID, &source.Ordinal, &source.SourceURL, &source.ResolvedURL, &source.Status, &source.FailureCode, &source.FailureDetail, &productID, &root, &skuID, &sku, &skuTitle, &skuURL, &price, &selected); err != nil {
			return Detail{}, err
		}
		current, ok := sources[source.ID]
		if !ok {
			source.Products = []Product{}
			sources[source.ID] = &source
			d.Sources = append(d.Sources, source)
			current = &d.Sources[len(d.Sources)-1]
		}
		if productID != nil {
			product := products[*productID]
			if product == nil {
				title := ""
				if skuTitle != nil {
					title = *skuTitle
				}
				product = &Product{SnapshotID: *productID, RootSKU: *root, Title: title}
				products[*productID] = product
				current.Products = append(current.Products, *product)
				product = &current.Products[len(current.Products)-1]
				products[*productID] = product
			}
			if skuID != nil {
				product.SKUs = append(product.SKUs, SKU{ID: *skuID, SKU: *sku, Title: *skuTitle, ResolvedURL: *skuURL, Price: price, Selected: *selected})
			}
		}
	}
	return d, rows.Err()
}

func (s *Service) UpdateSelection(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool, selectedIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND (owner_id=$2 OR $3))`, projectID, ownerID, isAdmin).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE project_sku_selections SET selected=false WHERE project_id=$1`, projectID); err != nil {
		return err
	}
	for _, id := range selectedIDs {
		tag, err := tx.Exec(ctx, `INSERT INTO project_sku_selections(project_id,snapshot_sku_id,selected) SELECT $1,ss.id,true FROM snapshot_skus ss JOIN product_snapshots p ON p.id=ss.snapshot_id JOIN project_sources ps ON ps.id=p.project_source_id WHERE ss.id=$2 AND ps.project_id=$1 ON CONFLICT(project_id,snapshot_sku_id) DO UPDATE SET selected=true`, projectID, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: selected sku is not in project", ErrInvalidInput)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE projects SET status='awaiting_sku_selection',updated_at=now() WHERE id=$1`, projectID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Retry(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool) error {
	command, err := s.pool.Exec(ctx, `UPDATE projects SET status='awaiting_extension',failure_code=NULL,failure_detail=NULL,updated_at=now() WHERE id=$1 AND status='failed' AND (owner_id=$2 OR $3)`, projectID, ownerID, isAdmin)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) RawExportRows(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool) ([][]string, [][]string, [][]string, error) {
	var allowed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND (owner_id=$2 OR $3))`, projectID, ownerID, isAdmin).Scan(&allowed); err != nil {
		return nil, nil, nil, err
	}
	if !allowed {
		return nil, nil, nil, ErrNotFound
	}
	skus := [][]string{{"输入链接", "解析链接", "商品标题", "SKU", "价格", "采集时间"}}
	specs := [][]string{{"SKU", "字段来源", "字段名", "字段值"}}
	images := [][]string{{"SKU", "图片类型", "序号", "原始 URL", "规范化 URL", "不可售"}}
	rows, err := s.pool.Query(ctx, `SELECT ps.source_url,ss.resolved_url,ss.title,ss.sku,COALESCE(ss.price,''),p.captured_at FROM snapshot_skus ss JOIN product_snapshots p ON p.id=ss.snapshot_id JOIN project_sources ps ON ps.id=p.project_source_id JOIN project_sku_selections sel ON sel.snapshot_sku_id=ss.id AND sel.project_id=ps.project_id AND sel.selected WHERE ps.project_id=$1 ORDER BY ps.ordinal,ss.ordinal`, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var a, b, c, d, e string
		var t time.Time
		if err := rows.Scan(&a, &b, &c, &d, &e, &t); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		skus = append(skus, []string{a, b, c, d, e, t.Format(time.RFC3339)})
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT ss.sku,sp.source,sp.name,sp.value FROM sku_specifications sp JOIN snapshot_skus ss ON ss.id=sp.snapshot_sku_id JOIN project_sku_selections sel ON sel.snapshot_sku_id=ss.id AND sel.selected WHERE sel.project_id=$1 ORDER BY ss.ordinal,sp.source,sp.ordinal`, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var a, b, c, d string
		if err := rows.Scan(&a, &b, &c, &d); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		specs = append(specs, []string{a, b, c, d})
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT COALESCE(ss.sku,''),i.image_type,i.ordinal,i.original_url,i.normalized_url,i.unavailable FROM sku_images i JOIN snapshot_skus ss ON ss.id=i.snapshot_sku_id JOIN product_snapshots p ON p.id=ss.snapshot_id JOIN project_sources ps ON ps.id=p.project_source_id JOIN project_sku_selections sel ON sel.snapshot_sku_id=ss.id AND sel.project_id=$1 WHERE ps.project_id=$1 AND sel.selected
UNION ALL SELECT '', 'unavailable_thumbnail', uv.ordinal, uv.thumbnail_url, COALESCE(uv.high_resolution_image_url, uv.thumbnail_url), true FROM unavailable_variants uv JOIN product_snapshots p ON p.id=uv.snapshot_id JOIN project_sources ps ON ps.id=p.project_source_id WHERE ps.project_id=$1 AND uv.thumbnail_url IS NOT NULL ORDER BY 1,2,3`, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var a, b, c, d, e string
		var unavailable bool
		if err := rows.Scan(&a, &b, &c, &d, &e, &unavailable); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		images = append(images, []string{a, b, c, d, e, fmt.Sprint(unavailable)})
	}
	rows.Close()
	return skus, specs, images, rows.Err()
}

type CaptureProduct struct {
	SKU          string              `json:"sku"`
	Title        string              `json:"title"`
	ResolvedURL  string              `json:"resolved_url"`
	Price        string              `json:"price"`
	Availability string              `json:"availability"`
	Summary      map[string]string   `json:"summary"`
	Parameters   map[string]string   `json:"parameters"`
	Images       map[string][]string `json:"images"`
}
type UnavailableVariant struct {
	Label                  string `json:"label"`
	ThumbnailURL           string `json:"thumbnail_url"`
	HighResolutionImageURL string `json:"high_resolution_image_url"`
}
type CaptureResult struct {
	SourceURL          string               `json:"source_url"`
	RootSKU            string               `json:"root_sku"`
	Products           []CaptureProduct     `json:"products"`
	UnresolvedVariants []UnavailableVariant `json:"unresolved_variants"`
}

func (s *Service) StoreCapture(ctx context.Context, taskID, extensionID uuid.UUID, capture CaptureResult) error {
	if len(capture.Products) == 0 || len(capture.Products) > 200 {
		return fmt.Errorf("%w: no products", ErrInvalidInput)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sourceID, projectID uuid.UUID
	var expectedURL string
	var status string
	err = tx.QueryRow(ctx, `SELECT ps.id,ps.project_id,ps.source_url,ct.status FROM capture_tasks ct JOIN capture_sessions cs ON cs.id=ct.capture_session_id JOIN project_sources ps ON ps.id=ct.project_source_id WHERE ct.id=$1 AND cs.extension_id=$2 FOR UPDATE`, taskID, extensionID).Scan(&sourceID, &projectID, &expectedURL, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "dispatched" {
		return ErrInvalidInput
	}
	if capture.SourceURL != "" && capture.SourceURL != expectedURL {
		return fmt.Errorf("%w: source url mismatch", ErrInvalidInput)
	}
	resolved := capture.Products[0].ResolvedURL
	raw, err := json.Marshal(capture)
	if err != nil {
		return err
	}
	var snapshotID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO product_snapshots(project_source_id,capture_task_id,source_url,resolved_url,root_sku,captured_at,raw_capture) VALUES($1,$2,$3,$4,$5,now(),$6) RETURNING id`, sourceID, taskID, expectedURL, resolved, capture.RootSKU, raw).Scan(&snapshotID)
	if err != nil {
		return err
	}
	for po, p := range capture.Products {
		if p.SKU == "" || p.Title == "" || p.ResolvedURL == "" {
			return fmt.Errorf("%w: product fields missing", ErrInvalidInput)
		}
		var skuID uuid.UUID
		err = tx.QueryRow(ctx, `INSERT INTO snapshot_skus(snapshot_id,sku,title,resolved_url,price,availability,ordinal) VALUES($1,$2,$3,$4,NULLIF($5,''),'available',$6) RETURNING id`, snapshotID, p.SKU, p.Title, p.ResolvedURL, p.Price, po).Scan(&skuID)
		if err != nil {
			return err
		}
		for source, values := range map[string]map[string]string{"summary": p.Summary, "parameters": p.Parameters} {
			i := 0
			for name, value := range values {
				if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
					continue
				}
				if _, err := tx.Exec(ctx, `INSERT INTO sku_specifications(snapshot_sku_id,source,name,value,ordinal) VALUES($1,$2,$3,$4,$5)`, skuID, source, name, value, i); err != nil {
					return err
				}
				i++
			}
		}
		for imageType, urls := range p.Images {
			if imageType != "main" && imageType != "variant_main" && imageType != "detail" {
				continue
			}
			for i, url := range urls {
				if !strings.HasPrefix(url, "http") {
					continue
				}
				if _, err := tx.Exec(ctx, `INSERT INTO sku_images(snapshot_sku_id,image_type,original_url,normalized_url,ordinal) VALUES($1,$2,$3,$3,$4)`, skuID, imageType, url, i); err != nil {
					return err
				}
			}
		}
		var sourceCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_sources WHERE project_id=$1`, projectID).Scan(&sourceCount); err != nil {
			return err
		}
		selected := sourceCount == 1 || p.SKU == capture.RootSKU
		if _, err := tx.Exec(ctx, `INSERT INTO project_sku_selections(project_id,snapshot_sku_id,selected) VALUES($1,$2,$3)`, projectID, skuID, selected); err != nil {
			return err
		}
	}
	for i, v := range capture.UnresolvedVariants {
		if _, err := tx.Exec(ctx, `INSERT INTO unavailable_variants(snapshot_id,label,thumbnail_url,high_resolution_image_url,ordinal) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),$5)`, snapshotID, v.Label, v.ThumbnailURL, v.HighResolutionImageURL, i); err != nil {
			return err
		}
		if v.ThumbnailURL != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO sku_images(image_type,original_url,normalized_url,ordinal,unavailable) VALUES('unavailable_thumbnail',$1,$1,$2,true)`, v.ThumbnailURL, i); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE project_sources SET resolved_url=$2,status='succeeded',updated_at=now() WHERE id=$1`, sourceID, resolved); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE capture_tasks SET status='succeeded',completed_at=now() WHERE id=$1`, taskID); err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM capture_tasks ct JOIN capture_sessions cs ON cs.id=ct.capture_session_id WHERE cs.project_id=$1 AND ct.status IN ('queued','dispatched')`, projectID).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		if _, err := tx.Exec(ctx, `UPDATE capture_sessions SET status='succeeded',completed_at=now() WHERE project_id=$1 AND status='running'`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE projects SET name=COALESCE(NULLIF(name,''),$2),status='awaiting_sku_selection',updated_at=now() WHERE id=$1`, projectID, capture.Products[0].Title); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
