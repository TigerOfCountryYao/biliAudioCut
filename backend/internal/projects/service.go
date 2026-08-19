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
	ErrNotFound          = errors.New("project not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrCaptureFailed     = errors.New("capture failed")
	ErrExportNotReady    = errors.New("project capture is not ready for export")
	ErrSelectionNotReady = errors.New("project capture is not ready for SKU selection")
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Project struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	FailureCode    *string   `json:"failure_code,omitempty"`
	FailureDetail  *string   `json:"failure_detail,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	CaptureAllSKUs bool      `json:"capture_all_skus"`
}

type Source struct {
	ID              uuid.UUID `json:"id"`
	Ordinal         int       `json:"ordinal"`
	SourceURL       string    `json:"source_url"`
	ResolvedURL     *string   `json:"resolved_url,omitempty"`
	Status          string    `json:"status"`
	FailureCode     *string   `json:"failure_code,omitempty"`
	FailureDetail   *string   `json:"failure_detail,omitempty"`
	InteractionKind *string   `json:"interaction_kind,omitempty"`
	Products        []Product `json:"products"`
}

type PendingJDAction struct {
	SourceID uuid.UUID
	Kind     string
	URL      string
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

func isExportReady(status string) bool {
	return status == "awaiting_sku_selection" || status == "failed" || status == "awaiting_jd_login" || status == "awaiting_jd_verification"
}
func canUpdateSelection(status string) bool { return status == "awaiting_sku_selection" }

func normalizeLinks(links []string) ([]string, error) {
	unique := make([]string, 0, len(links))
	seen := map[string]bool{}
	for _, raw := range links {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || !isAllowedJDSourceURL(parsed) {
			return nil, fmt.Errorf("%w: link must be a JD item, short, or affiliate link", ErrInvalidInput)
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

func isAllowedJDSourceURL(parsed *url.URL) bool {
	switch parsed.Hostname() {
	case "item.jd.com", "u.jd.com":
		return true
	case "union-click.jd.com":
		return parsed.Path == "/jdc"
	default:
		return false
	}
}

func shortLink(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() != "u.jd.com" {
		return ""
	}
	return raw
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name string, links []string, captureAllSKUs bool) (Project, error) {
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
	err = tx.QueryRow(ctx, `INSERT INTO projects(owner_id,name,status,capture_all_skus) VALUES($1,NULLIF($2,''),'awaiting_extension',$3) RETURNING id,COALESCE(name,''),status,failure_code,failure_detail,created_at,updated_at,capture_all_skus`, ownerID, name, captureAllSKUs).Scan(&project.ID, &project.Name, &project.Status, &project.FailureCode, &project.FailureDetail, &project.CreatedAt, &project.UpdatedAt, &project.CaptureAllSKUs)
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
	query := `SELECT id,COALESCE(name,''),status,failure_code,failure_detail,created_at,updated_at,capture_all_skus FROM projects WHERE owner_id=$1 OR $2 ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query, ownerID, isAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Project, 0)
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Status, &p.FailureCode, &p.FailureDetail, &p.CreatedAt, &p.UpdatedAt, &p.CaptureAllSKUs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Delete removes a project the user owns. Related capture records are removed
// by the database foreign-key cascade.
func (s *Service) Delete(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool) error {
	command, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1 AND (owner_id=$2 OR $3)`, projectID, ownerID, isAdmin)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Get(ctx context.Context, id, ownerID uuid.UUID, isAdmin bool) (Detail, error) {
	var d Detail
	err := s.pool.QueryRow(ctx, `SELECT id,COALESCE(name,''),status,failure_code,failure_detail,created_at,updated_at,capture_all_skus FROM projects WHERE id=$1 AND (owner_id=$2 OR $3)`, id, ownerID, isAdmin).Scan(&d.Project.ID, &d.Project.Name, &d.Project.Status, &d.Project.FailureCode, &d.Project.FailureDetail, &d.Project.CreatedAt, &d.Project.UpdatedAt, &d.Project.CaptureAllSKUs)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT ps.id,ps.ordinal,ps.source_url,ps.resolved_url,ps.status,ps.failure_code,ps.failure_detail,ps.interaction_kind, p.id,p.root_sku,ss.id,ss.sku,ss.title,ss.resolved_url,ss.price,ss.variant_label,ss.series_label,ss.series_ordinal,COALESCE(sel.selected,false)
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
		if err := rows.Scan(&source.ID, &source.Ordinal, &source.SourceURL, &source.ResolvedURL, &source.Status, &source.FailureCode, &source.FailureDetail, &source.InteractionKind, &productID, &root, &skuID, &sku, &skuTitle, &skuURL, &price, &variantLabel, &seriesLabel, &seriesOrdinal, &selected); err != nil {
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
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM projects WHERE id=$1 AND (owner_id=$2 OR $3) FOR UPDATE`, projectID, ownerID, isAdmin).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if !canUpdateSelection(status) {
		return ErrSelectionNotReady
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
	_, err = tx.Exec(ctx, `UPDATE projects SET updated_at=now() WHERE id=$1`, projectID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Retry(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	command, err := tx.Exec(ctx, `UPDATE projects SET status='awaiting_extension',failure_code=NULL,failure_detail=NULL,updated_at=now() WHERE id=$1 AND status IN ('failed','awaiting_jd_login','awaiting_jd_verification') AND (owner_id=$2 OR $3)`, projectID, ownerID, isAdmin)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	// Reflect the resumed work immediately. StartCapture creates queued tasks
	// from these sources right after Retry returns, while successful sources stay
	// untouched and are never captured again.
	if _, err := tx.Exec(ctx, `UPDATE project_sources SET status='collecting',failure_code=NULL,failure_detail=NULL,interaction_kind=NULL,interaction_url=NULL,updated_at=now() WHERE project_id=$1 AND status='failed'`, projectID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) PendingJDAction(ctx context.Context, projectID, sourceID, ownerID uuid.UUID, isAdmin bool) (PendingJDAction, error) {
	var action PendingJDAction
	err := s.pool.QueryRow(ctx, `SELECT ps.id,COALESCE(ps.interaction_kind,''),COALESCE(ps.interaction_url,'')
FROM project_sources ps JOIN projects p ON p.id=ps.project_id
WHERE ps.id=$1 AND ps.project_id=$2 AND (p.owner_id=$3 OR $4) AND p.status IN ('awaiting_jd_login','awaiting_jd_verification')`, sourceID, projectID, ownerID, isAdmin).Scan(&action.SourceID, &action.Kind, &action.URL)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingJDAction{}, ErrNotFound
	}
	if err != nil {
		return PendingJDAction{}, err
	}
	if action.URL == "" || (action.Kind != "login" && action.Kind != "verification") {
		return PendingJDAction{}, ErrNotFound
	}
	return action, nil
}

type Export struct {
	ProjectName string
	Rows        [][]string
}

// MainImageExport contains the first high-resolution variant main image of
// every successfully captured SKU in a project. Image selection deliberately
// does not follow the Excel selection: this is the project's image archive.
type MainImageExport struct {
	ProjectName string
	Images      []MainImage
}

type MainImage struct {
	SKU          string
	SeriesLabel  string
	VariantLabel string
	URLs         []string
}

func (s *Service) ExportRows(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool) (Export, error) {
	var status, projectName string
	err := s.pool.QueryRow(ctx, `SELECT status,COALESCE(name,'') FROM projects WHERE id=$1 AND (owner_id=$2 OR $3)`, projectID, ownerID, isAdmin).Scan(&status, &projectName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Export{}, ErrNotFound
	}
	if err != nil {
		return Export{}, err
	}
	if !isExportReady(status) {
		return Export{}, ErrExportNotReady
	}
	type selectedSKU struct {
		id     uuid.UUID
		values []string
	}
	selectedSKUs := make([]selectedSKU, 0)
	rows, err := s.pool.Query(ctx, `SELECT ss.id,ps.source_url,ss.series_label,ss.variant_label,ss.title,ss.sku,COALESCE(ss.price,''),ss.resolved_url,COALESCE(image.normalized_url,'')
FROM snapshot_skus ss
JOIN product_snapshots p ON p.id=ss.snapshot_id
JOIN project_sources ps ON ps.id=p.project_source_id
JOIN project_sku_selections sel ON sel.snapshot_sku_id=ss.id AND sel.project_id=ps.project_id AND sel.selected
LEFT JOIN LATERAL (SELECT normalized_url FROM sku_images WHERE snapshot_sku_id=ss.id AND image_type='variant_main' ORDER BY ordinal LIMIT 1) image ON true
WHERE ps.project_id=$1 ORDER BY ps.ordinal,ss.ordinal`, projectID)
	if err != nil {
		return Export{}, err
	}
	for rows.Next() {
		var skuID uuid.UUID
		var sourceURL, series, variant, title, sku, price, longURL, imageURL string
		if err := rows.Scan(&skuID, &sourceURL, &series, &variant, &title, &sku, &price, &longURL, &imageURL); err != nil {
			rows.Close()
			return Export{}, err
		}
		selectedSKUs = append(selectedSKUs, selectedSKU{id: skuID, values: []string{series, variant, title, sku, price, shortLink(sourceURL), longURL, imageURL}})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Export{}, err
	}
	rows.Close()
	fieldHeaders := make([]string, 0)
	fieldOrder := make([]string, 0)
	valuesByField := make(map[string]map[uuid.UUID]string)
	rows, err = s.pool.Query(ctx, `SELECT sp.snapshot_sku_id,sp.source,sp.name,sp.value
FROM sku_specifications sp
JOIN project_sku_selections sel ON sel.snapshot_sku_id=sp.snapshot_sku_id AND sel.selected
WHERE sel.project_id=$1
ORDER BY CASE sp.source WHEN 'summary' THEN 0 ELSE 1 END,sp.name,sp.ordinal`, projectID)
	if err != nil {
		return Export{}, err
	}
	for rows.Next() {
		var skuID uuid.UUID
		var source, name, value string
		if err := rows.Scan(&skuID, &source, &name, &value); err != nil {
			rows.Close()
			return Export{}, err
		}
		key := source + "\x00" + name
		if _, exists := valuesByField[key]; !exists {
			valuesByField[key] = make(map[uuid.UUID]string)
			fieldOrder = append(fieldOrder, key)
			fieldHeaders = append(fieldHeaders, specificationSourceLabel(source)+"："+name)
		}
		valuesByField[key][skuID] = value
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Export{}, err
	}
	rows.Close()
	headers := append([]string{"系列品", "款式名称", "商品标题", "SKU", "价格", "短链", "长链", "款式主图 URL"}, fieldHeaders...)
	result := Export{ProjectName: projectName, Rows: [][]string{headers}}
	for _, sku := range selectedSKUs {
		row := append([]string{}, sku.values...)
		for _, key := range fieldOrder {
			row = append(row, valuesByField[key][sku.id])
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func (s *Service) MainImages(ctx context.Context, projectID, ownerID uuid.UUID, isAdmin bool) (MainImageExport, error) {
	var status, projectName string
	err := s.pool.QueryRow(ctx, `SELECT status,COALESCE(name,'') FROM projects WHERE id=$1 AND (owner_id=$2 OR $3)`, projectID, ownerID, isAdmin).Scan(&status, &projectName)
	if errors.Is(err, pgx.ErrNoRows) {
		return MainImageExport{}, ErrNotFound
	}
	if err != nil {
		return MainImageExport{}, err
	}
	if !isExportReady(status) {
		return MainImageExport{}, ErrExportNotReady
	}

	rows, err := s.pool.Query(ctx, `SELECT ss.id,ss.sku,ss.series_label,ss.variant_label,image.normalized_url
FROM snapshot_skus ss
JOIN product_snapshots p ON p.id=ss.snapshot_id
JOIN project_sources ps ON ps.id=p.project_source_id
JOIN sku_images image ON image.snapshot_sku_id=ss.id AND image.image_type='variant_main'
WHERE ps.project_id=$1
ORDER BY ps.ordinal,ss.ordinal,image.ordinal`, projectID)
	if err != nil {
		return MainImageExport{}, err
	}
	defer rows.Close()

	export := MainImageExport{ProjectName: projectName}
	imageIndexes := make(map[uuid.UUID]int)
	for rows.Next() {
		var snapshotSKUId uuid.UUID
		var sku, seriesLabel, variantLabel, imageURL string
		if err := rows.Scan(&snapshotSKUId, &sku, &seriesLabel, &variantLabel, &imageURL); err != nil {
			return MainImageExport{}, err
		}
		if imageIndex, exists := imageIndexes[snapshotSKUId]; exists {
			export.Images[imageIndex].URLs = append(export.Images[imageIndex].URLs, imageURL)
			continue
		}
		imageIndexes[snapshotSKUId] = len(export.Images)
		export.Images = append(export.Images, MainImage{SKU: sku, SeriesLabel: seriesLabel, VariantLabel: variantLabel, URLs: []string{imageURL}})
	}
	if err := rows.Err(); err != nil {
		return MainImageExport{}, err
	}
	return export, nil
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
	var sourceID, projectID, sessionID uuid.UUID
	var expectedURL string
	var status string
	err = tx.QueryRow(ctx, `SELECT ps.id,ps.project_id,cs.id,ps.source_url,ct.status FROM capture_tasks ct JOIN capture_sessions cs ON cs.id=ct.capture_session_id JOIN project_sources ps ON ps.id=ct.project_source_id WHERE ct.id=$1 AND cs.extension_id=$2 FOR UPDATE`, taskID, extensionID).Scan(&sourceID, &projectID, &sessionID, &expectedURL, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "succeeded" {
		return nil
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
		if _, err := tx.Exec(ctx, `INSERT INTO project_sku_selections(project_id,snapshot_sku_id,selected) VALUES($1,$2,true)`, projectID, skuID); err != nil {
			return err
		}
	}
	for i, v := range capture.UnresolvedVariants {
		if _, err := tx.Exec(ctx, `INSERT INTO unavailable_variants(snapshot_id,label,series_label,series_ordinal,thumbnail_url,high_resolution_image_url,ordinal) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7)`, snapshotID, v.Label, v.SeriesLabel, v.SeriesOrdinal, v.ThumbnailURL, v.HighResolutionImageURL, i); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE project_sources SET resolved_url=$2,status='succeeded',failure_code=NULL,failure_detail=NULL,interaction_kind=NULL,interaction_url=NULL,updated_at=now() WHERE id=$1`, sourceID, resolved); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE capture_tasks SET status='succeeded',failure_code=NULL,failure_detail=NULL,completed_at=now() WHERE id=$1`, taskID); err != nil {
		return err
	}
	var remaining, failed int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status IN ('queued','dispatched')),count(*) FILTER (WHERE status='failed') FROM capture_tasks WHERE capture_session_id=$1`, sessionID).Scan(&remaining, &failed); err != nil {
		return err
	}
	if remaining == 0 {
		finalStatus := "succeeded"
		projectStatus := "awaiting_sku_selection"
		if failed > 0 {
			finalStatus = "failed"
			projectStatus = "failed"
		}
		if _, err := tx.Exec(ctx, `UPDATE capture_sessions SET status=$2,completed_at=now() WHERE id=$1 AND status='running'`, sessionID, finalStatus); err != nil {
			return err
		}
		if failed > 0 {
			if _, err := tx.Exec(ctx, `UPDATE projects SET status=$2,updated_at=now() WHERE id=$1 AND status='collecting'`, projectID, projectStatus); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE projects SET name=COALESCE(NULLIF(name,''),$2),status=$3,failure_code=NULL,failure_detail=NULL,updated_at=now() WHERE id=$1 AND status='collecting'`, projectID, capture.Products[0].Title, projectStatus); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
