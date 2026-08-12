package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxLinksPerProject = 20

var (
	ErrNotFound       = errors.New("project not found")
	ErrInvalidInput   = errors.New("invalid input")
	ErrCaptureFailed  = errors.New("capture failed")
	ErrExportNotReady = errors.New("project capture is not ready for export")
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
	ID            uuid.UUID `json:"id"`
	SKU           string    `json:"sku"`
	Title         string    `json:"title"`
	ResolvedURL   string    `json:"resolved_url"`
	Price         *string   `json:"price,omitempty"`
	VariantLabel  string    `json:"variant_label"`
	SeriesLabel   string    `json:"series_label"`
	SeriesOrdinal int       `json:"series_ordinal"`
	Selected      bool      `json:"selected"`
}

type Detail struct {
	Project Project  `json:"project"`
	Sources []Source `json:"sources"`
}

func isExportReady(status string) bool { return status == "awaiting_sku_selection" }

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
	out := make([]Project, 0)
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
	rows, err := s.pool.Query(ctx, `SELECT ps.id,ps.ordinal,ps.source_url,ps.resolved_url,ps.status,ps.failure_code,ps.failure_detail, p.id,p.root_sku,ss.id,ss.sku,ss.title,ss.resolved_url,ss.price,ss.variant_label,ss.series_label,ss.series_ordinal,COALESCE(sel.selected,false)
FROM project_sources ps LEFT JOIN product_snapshots p ON p.project_source_id=ps.id LEFT JOIN snapshot_skus ss ON ss.snapshot_id=p.id LEFT JOIN project_sku_selections sel ON sel.project_id=ps.project_id AND sel.snapshot_sku_id=ss.id WHERE ps.project_id=$1 ORDER BY ps.ordinal,ss.ordinal`, id)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	sourceIndexes := map[uuid.UUID]int{}
	productIndexes := map[uuid.UUID]int{}
	for rows.Next() {
		var source Source
		var productID *uuid.UUID
		var root *string
		var skuID *uuid.UUID
		var sku, skuTitle, skuURL, price, variantLabel, seriesLabel *string
		var seriesOrdinal *int
		var selected *bool
		if err := rows.Scan(&source.ID, &source.Ordinal, &source.SourceURL, &source.ResolvedURL, &source.Status, &source.FailureCode, &source.FailureDetail, &productID, &root, &skuID, &sku, &skuTitle, &skuURL, &price, &variantLabel, &seriesLabel, &seriesOrdinal, &selected); err != nil {
			return Detail{}, err
		}
		sourceIndex, ok := sourceIndexes[source.ID]
		if !ok {
			source.Products = []Product{}
			d.Sources = append(d.Sources, source)
			sourceIndex = len(d.Sources) - 1
			sourceIndexes[source.ID] = sourceIndex
		}
		if productID != nil {
			productIndex, exists := productIndexes[*productID]
			if !exists {
				title := ""
				if skuTitle != nil {
					title = *skuTitle
				}
				d.Sources[sourceIndex].Products = append(d.Sources[sourceIndex].Products, Product{SnapshotID: *productID, RootSKU: *root, Title: title})
				productIndex = len(d.Sources[sourceIndex].Products) - 1
				productIndexes[*productID] = productIndex
			}
			if skuID != nil {
				series := "默认系列"
				if seriesLabel != nil && strings.TrimSpace(*seriesLabel) != "" {
					series = *seriesLabel
				}
				ordinal := 0
				if seriesOrdinal != nil {
					ordinal = *seriesOrdinal
				}
				variant := ""
				if variantLabel != nil {
					variant = *variantLabel
				}
				d.Sources[sourceIndex].Products[productIndex].SKUs = append(d.Sources[sourceIndex].Products[productIndex].SKUs, SKU{ID: *skuID, SKU: *sku, Title: *skuTitle, ResolvedURL: *skuURL, Price: price, VariantLabel: variant, SeriesLabel: series, SeriesOrdinal: ordinal, Selected: *selected})
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
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM projects WHERE id=$1 AND (owner_id=$2 OR $3)`, projectID, ownerID, isAdmin).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if !isExportReady(status) {
		return nil, nil, nil, ErrExportNotReady
	}
	skus := [][]string{{"输入链接", "解析链接", "系列品", "款式名称", "商品标题", "SKU", "价格", "采集时间"}}
	images := [][]string{{"系列品", "款式名称", "SKU", "图片类型", "原始 URL", "规范化 URL"}}
	type selectedSKU struct {
		id   uuid.UUID
		name string
		sku  string
	}
	selectedSKUs := make([]selectedSKU, 0)
	rows, err := s.pool.Query(ctx, `SELECT ss.id,ps.source_url,ss.resolved_url,ss.series_label,ss.variant_label,ss.title,ss.sku,COALESCE(ss.price,''),p.captured_at FROM snapshot_skus ss JOIN product_snapshots p ON p.id=ss.snapshot_id JOIN project_sources ps ON ps.id=p.project_source_id JOIN project_sku_selections sel ON sel.snapshot_sku_id=ss.id AND sel.project_id=ps.project_id AND sel.selected WHERE ps.project_id=$1 ORDER BY ps.ordinal,ss.ordinal`, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var skuID uuid.UUID
		var a, b, c, d, e, f, g string
		var t time.Time
		if err := rows.Scan(&skuID, &a, &b, &c, &d, &e, &f, &g, &t); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		name := d
		if name == "" {
			name = e
		}
		selectedSKUs = append(selectedSKUs, selectedSKU{id: skuID, name: name, sku: f})
		skus = append(skus, []string{a, b, c, d, e, f, g, t.Format(time.RFC3339)})
	}
	rows.Close()
	specs := make([][]string, 0)
	specHeaders := []string{"字段来源", "字段名"}
	for _, sku := range selectedSKUs {
		specHeaders = append(specHeaders, fmt.Sprintf("%s（SKU：%s）", sku.name, sku.sku))
	}
	specs = append(specs, specHeaders)
	valuesByField := make(map[string]map[uuid.UUID]string)
	fieldOrder := make([]string, 0)
	rows, err = s.pool.Query(ctx, `SELECT sp.snapshot_sku_id,sp.source,sp.name,sp.value FROM sku_specifications sp JOIN project_sku_selections sel ON sel.snapshot_sku_id=sp.snapshot_sku_id AND sel.selected WHERE sel.project_id=$1 ORDER BY sp.source,sp.name,sp.ordinal`, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var skuID uuid.UUID
		var source, name, value string
		if err := rows.Scan(&skuID, &source, &name, &value); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		key := source + "\x00" + name
		if _, exists := valuesByField[key]; !exists {
			valuesByField[key] = make(map[uuid.UUID]string)
			fieldOrder = append(fieldOrder, key)
		}
		valuesByField[key][skuID] = value
	}
	rows.Close()
	for _, key := range fieldOrder {
		parts := strings.SplitN(key, "\x00", 2)
		row := []string{specificationSourceLabel(parts[0]), parts[1]}
		for _, sku := range selectedSKUs {
			row = append(row, valuesByField[key][sku.id])
		}
		specs = append(specs, row)
	}
	rows, err = s.pool.Query(ctx, `SELECT ss.series_label,ss.variant_label,ss.sku,i.original_url,i.normalized_url FROM sku_images i JOIN snapshot_skus ss ON ss.id=i.snapshot_sku_id JOIN product_snapshots p ON p.id=ss.snapshot_id JOIN project_sources ps ON ps.id=p.project_source_id JOIN project_sku_selections sel ON sel.snapshot_sku_id=ss.id AND sel.project_id=ps.project_id AND sel.selected WHERE ps.project_id=$1 AND i.image_type='variant_main' ORDER BY ps.ordinal,ss.ordinal,i.ordinal`, projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var a, b, c, d, e string
		if err := rows.Scan(&a, &b, &c, &d, &e); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		images = append(images, []string{a, b, c, "款式主图", d, e})
	}
	rows.Close()
	return skus, specs, images, rows.Err()
}

func specificationSourceLabel(source string) string {
	if source == "summary" {
		return "摘要"
	}
	return "规格表"
}

type CaptureProduct struct {
	SKU           string              `json:"sku"`
	Title         string              `json:"title"`
	ResolvedURL   string              `json:"resolved_url"`
	Price         string              `json:"price"`
	Availability  string              `json:"availability"`
	VariantLabel  string              `json:"variant_label"`
	SeriesLabel   string              `json:"series_label"`
	SeriesOrdinal int                 `json:"series_ordinal"`
	Summary       map[string]string   `json:"summary"`
	Parameters    map[string]string   `json:"parameters"`
	Images        map[string][]string `json:"images"`
}
type UnavailableVariant struct {
	Label                  string `json:"label"`
	SeriesLabel            string `json:"series_label"`
	SeriesOrdinal          int    `json:"series_ordinal"`
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
		err = tx.QueryRow(ctx, `INSERT INTO snapshot_skus(snapshot_id,sku,title,resolved_url,price,availability,variant_label,series_label,series_ordinal,ordinal) VALUES($1,$2,$3,$4,NULLIF($5,''),'available',$6,$7,$8,$9) RETURNING id`, snapshotID, p.SKU, p.Title, p.ResolvedURL, p.Price, p.VariantLabel, p.SeriesLabel, p.SeriesOrdinal, po).Scan(&skuID)
		if err != nil {
			return err
		}
		for source, values := range map[string]map[string]string{"summary": p.Summary, "parameters": p.Parameters} {
			names := make([]string, 0, len(values))
			for name := range values {
				names = append(names, name)
			}
			sort.Strings(names)
			i := 0
			for _, name := range names {
				value := values[name]
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
			if imageType != "variant_main" {
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
		if _, err := tx.Exec(ctx, `INSERT INTO unavailable_variants(snapshot_id,label,series_label,series_ordinal,thumbnail_url,high_resolution_image_url,ordinal) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7)`, snapshotID, v.Label, v.SeriesLabel, v.SeriesOrdinal, v.ThumbnailURL, v.HighResolutionImageURL, i); err != nil {
			return err
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
