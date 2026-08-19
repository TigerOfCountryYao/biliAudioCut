package projects

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/extensions"
	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/identity"
	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/production"
	"github.com/google/uuid"
)

type HTTPHandler struct {
	service    *Service
	extensions *extensions.Service
	hub        *extensions.Hub
}

func NewHTTPHandler(service *Service, extensionsService *extensions.Service, hub *extensions.Hub) *HTTPHandler {
	return &HTTPHandler{service: service, extensions: extensionsService, hub: hub}
}
func (h *HTTPHandler) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler) {
	mux.Handle("POST /api/projects", protect(http.HandlerFunc(h.create)))
	mux.Handle("GET /api/projects", protect(http.HandlerFunc(h.list)))
	mux.Handle("GET /api/projects/{projectId}", protect(http.HandlerFunc(h.get)))
	mux.Handle("DELETE /api/projects/{projectId}", protect(http.HandlerFunc(h.deleteProject)))
	mux.Handle("PUT /api/projects/{projectId}/sku-selection", protect(http.HandlerFunc(h.selection)))
	mux.Handle("POST /api/projects/{projectId}/retry", protect(http.HandlerFunc(h.retry)))
	mux.Handle("POST /api/projects/{projectId}/sources/{sourceId}/open-jd-action", protect(http.HandlerFunc(h.openJDAction)))
	mux.Handle("GET /api/projects/{projectId}/export.xlsx", protect(http.HandlerFunc(h.export)))
	mux.Handle("GET /api/projects/{projectId}/main-images.zip", protect(http.HandlerFunc(h.mainImages)))
	mux.Handle("POST /api/extension/authorization-codes", protect(http.HandlerFunc(h.createAuthorizationCode)))
	mux.Handle("GET /api/extension/device-status", protect(http.HandlerFunc(h.extensionDeviceStatus)))
	mux.HandleFunc("POST /api/extension/token", h.exchangeToken)
	mux.HandleFunc("DELETE /api/extension/device", h.disconnectDevice)
	mux.HandleFunc("POST /api/extension/capture-results", h.captureResult)
	mux.HandleFunc("POST /api/extension/capture-failures", h.captureFailure)
	mux.Handle("GET /api/extension/authorize", protect(http.HandlerFunc(h.authorizeRedirect)))
}

func (h *HTTPHandler) extensionDeviceStatus(w http.ResponseWriter, r *http.Request) {
	u, _ := current(r)
	status, err := h.extensions.StatusForUser(r.Context(), u.ID)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	respond(w, http.StatusOK, status)
}

func (h *HTTPHandler) retry(w http.ResponseWriter, r *http.Request) {
	id, err := projectID(r)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid project id"})
		return
	}
	u, _ := current(r)
	if err := h.service.Retry(r.Context(), id, u.ID, isAdmin(u)); errors.Is(err, ErrNotFound) {
		respond(w, 404, map[string]string{"error": "project is not retryable"})
		return
	} else if err != nil {
		respond(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	if _, err := h.extensions.StartCapture(r.Context(), id, u.ID); err != nil {
		respond(w, 500, map[string]string{"error": "could not schedule capture"})
		return
	}
	h.hub.DispatchForUser(r.Context(), u.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) openJDAction(w http.ResponseWriter, r *http.Request) {
	projectID, err := projectID(r)
	if err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid project id"})
		return
	}
	sourceID, err := uuid.Parse(r.PathValue("sourceId"))
	if err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid source id"})
		return
	}
	u, _ := current(r)
	action, err := h.service.PendingJDAction(r.Context(), projectID, sourceID, u.ID, isAdmin(u))
	if errors.Is(err, ErrNotFound) {
		respond(w, http.StatusNotFound, map[string]string{"error": "no pending JD action for this link"})
		return
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if !h.hub.OpenJDActionForUser(r.Context(), u.ID, action.SourceID, action.URL) {
		respond(w, http.StatusConflict, map[string]string{"error": "Chrome 扩展未连接，无法打开京东处理页面"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) authorizeRedirect(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("code_challenge")
	redirectURI := r.URL.Query().Get("redirect_uri")
	u, _ := current(r)
	code, err := h.extensions.CreateAuthorizationCode(r.Context(), u.ID, challenge, redirectURI)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid authorization request"})
		return
	}
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	http.Redirect(w, r, redirectURI+separator+"code="+code, http.StatusFound)
}

func extensionToken(r *http.Request) string {
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}

func (h *HTTPHandler) exchangeToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		DeviceName   string `json:"device_name"`
	}
	if err := decode(r, &in); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	device, err := h.extensions.ExchangeAuthorizationCode(r.Context(), in.Code, in.CodeVerifier, in.DeviceName)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid authorization code"})
		return
	}
	// A user may bind only one extension device. A previous WebSocket was
	// authenticated before its token was replaced, so close it explicitly.
	h.hub.Disconnect(device.ID)
	respond(w, 201, map[string]string{"access_token": device.Token, "token_type": "Bearer"})
}

func (h *HTTPHandler) disconnectDevice(w http.ResponseWriter, r *http.Request) {
	token := extensionToken(r)
	device, err := h.extensions.DeviceForToken(r.Context(), token)
	if err != nil {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if err := h.extensions.Disconnect(r.Context(), token); err != nil {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	h.hub.Disconnect(device.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) captureResult(w http.ResponseWriter, r *http.Request) {
	device, err := h.extensions.DeviceForToken(r.Context(), extensionToken(r))
	if err != nil {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	var in struct {
		TaskID  uuid.UUID     `json:"task_id"`
		Capture CaptureResult `json:"capture"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond(w, 400, map[string]string{"error": "invalid capture result"})
		return
	}
	if err := h.service.StoreCapture(r.Context(), in.TaskID, device.ID, in.Capture); err != nil {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.hub.Dispatch(r.Context(), device.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) captureFailure(w http.ResponseWriter, r *http.Request) {
	device, err := h.extensions.DeviceForToken(r.Context(), extensionToken(r))
	if err != nil {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	var in struct {
		TaskID         uuid.UUID `json:"task_id"`
		Code           string    `json:"code"`
		Detail         string    `json:"detail"`
		InteractionURL string    `json:"interaction_url"`
	}
	if err := decode(r, &in); err != nil || in.TaskID == uuid.Nil || strings.TrimSpace(in.Code) == "" {
		respond(w, 400, map[string]string{"error": "invalid failure"})
		return
	}
	continueCapture, err := h.extensions.FailTask(r.Context(), in.TaskID, device.ID, in.Code, in.Detail, in.InteractionURL)
	if err != nil {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if continueCapture {
		h.hub.Dispatch(r.Context(), device.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
func current(r *http.Request) (identitysqlcUser, error) {
	u, ok := identity.CurrentUserFromContext(r.Context())
	if !ok {
		return identitysqlcUser{}, errors.New("no user")
	}
	return identitysqlcUser{ID: u.ID, Role: u.Role}, nil
}

type identitysqlcUser struct {
	ID   uuid.UUID
	Role string
}

func isAdmin(u identitysqlcUser) bool { return u.Role == "admin" }
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string   `json:"name"`
		Links          []string `json:"links"`
		CaptureAllSKUs bool     `json:"capture_all_skus"`
	}
	if err := decode(r, &in); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	u, _ := current(r)
	p, err := h.service.Create(r.Context(), u.ID, in.Name, in.Links, in.CaptureAllSKUs)
	if errors.Is(err, ErrInvalidInput) {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		respond(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	if _, err := h.extensions.StartCapture(r.Context(), p.ID, u.ID); err != nil {
		respond(w, 500, map[string]string{"error": "could not schedule capture"})
		return
	}
	h.hub.DispatchForUser(r.Context(), u.ID)
	respond(w, 201, p)
}
func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	u, _ := current(r)
	items, err := h.service.List(r.Context(), u.ID, isAdmin(u))
	if err != nil {
		respond(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	respond(w, 200, map[string]any{"projects": items})
}
func projectID(r *http.Request) (uuid.UUID, error) { return uuid.Parse(r.PathValue("projectId")) }
func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := projectID(r)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid project id"})
		return
	}
	u, _ := current(r)
	d, err := h.service.Get(r.Context(), id, u.ID, isAdmin(u))
	if errors.Is(err, ErrNotFound) {
		respond(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		respond(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	respond(w, 200, d)
}

func (h *HTTPHandler) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := projectID(r)
	if err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid project id"})
		return
	}
	u, _ := current(r)
	if err := h.service.Delete(r.Context(), id, u.ID, isAdmin(u)); errors.Is(err, ErrNotFound) {
		respond(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	} else if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) selection(w http.ResponseWriter, r *http.Request) {
	id, err := projectID(r)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid project id"})
		return
	}
	var in struct {
		SelectedSKUIds []uuid.UUID `json:"selected_sku_ids"`
	}
	if err := decode(r, &in); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	u, _ := current(r)
	if err := h.service.UpdateSelection(r.Context(), id, u.ID, isAdmin(u), in.SelectedSKUIds); errors.Is(err, ErrNotFound) {
		respond(w, 404, map[string]string{"error": "not found"})
		return
	} else if errors.Is(err, ErrSelectionNotReady) {
		respond(w, http.StatusConflict, map[string]string{"error": "capture is not complete"})
		return
	} else if err != nil {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *HTTPHandler) export(w http.ResponseWriter, r *http.Request) {
	id, err := projectID(r)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid project id"})
		return
	}
	u, _ := current(r)
	export, err := h.service.ExportRows(r.Context(), id, u.ID, isAdmin(u))
	if errors.Is(err, ErrNotFound) {
		respond(w, 404, map[string]string{"error": "not found"})
		return
	}
	if errors.Is(err, ErrExportNotReady) {
		respond(w, http.StatusConflict, map[string]string{"error": "capture is not complete"})
		return
	}
	if err != nil {
		respond(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	file, err := production.BuildCaptureWorkbook(export.Rows)
	if err != nil {
		respond(w, 500, map[string]string{"error": "export failed"})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", exportFilename(export.ProjectName, time.Now())))
	_, _ = w.Write(file)
}

const maxMainImageBytes int64 = 25 << 20

var mainImageHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(request *http.Request, _ []*http.Request) error {
		if !isJDImageURL(request.URL.String()) {
			return errors.New("image redirect is outside JD image hosts")
		}
		return nil
	},
}

func (h *HTTPHandler) mainImages(w http.ResponseWriter, r *http.Request) {
	id, err := projectID(r)
	if err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid project id"})
		return
	}
	u, _ := current(r)
	export, err := h.service.MainImages(r.Context(), id, u.ID, isAdmin(u))
	if errors.Is(err, ErrNotFound) {
		respond(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if errors.Is(err, ErrExportNotReady) {
		respond(w, http.StatusConflict, map[string]string{"error": "capture is not complete"})
		return
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if len(export.Images) == 0 {
		respond(w, http.StatusConflict, map[string]string{"error": "no captured main images"})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", mainImageArchiveFilename(export.ProjectName, time.Now())))
	archive := zip.NewWriter(w)
	failures := make([]string, 0)
	for index, image := range export.Images {
		if err := writeFirstMainImage(r.Context(), archive, export.ProjectName, index, image); err != nil {
			failures = append(failures, fmt.Sprintf("%s\t%s\t%s", image.SKU, strings.Join(image.URLs, " | "), err))
		}
	}
	if len(failures) > 0 {
		entry, err := archive.Create(mainImageFolderName(export.ProjectName) + "/下载失败清单.txt")
		if err == nil {
			_, _ = io.WriteString(entry, "以下主图未能下载，可根据 URL 手动查看：\n\n"+strings.Join(failures, "\n"))
		}
	}
	_ = archive.Close()
}

func writeFirstMainImage(ctx context.Context, archive *zip.Writer, projectName string, index int, image MainImage) error {
	if len(image.URLs) == 0 {
		return errors.New("no main-image candidates")
	}
	failed := make([]string, 0, len(image.URLs))
	for _, imageURL := range image.URLs {
		if err := writeMainImageCandidate(ctx, archive, projectName, index, image, imageURL); err == nil {
			return nil
		} else {
			failed = append(failed, err.Error())
		}
	}
	return fmt.Errorf("all main-image candidates failed: %s", strings.Join(failed, "; "))
}

func writeMainImageCandidate(ctx context.Context, archive *zip.Writer, projectName string, index int, image MainImage, imageURL string) error {
	if !isJDImageURL(imageURL) {
		return errors.New("image URL is not an allowed JD image host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return err
	}
	response, err := mainImageHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("image server returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxMainImageBytes {
		return fmt.Errorf("image exceeds %d MB", maxMainImageBytes>>20)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("candidate is not an image (%s)", contentType)
	}
	entry, err := archive.Create(mainImageEntryName(projectName, index, image, imageURL, contentType))
	if err != nil {
		return err
	}
	written, err := io.Copy(entry, io.LimitReader(response.Body, maxMainImageBytes+1))
	if err != nil {
		return err
	}
	if written > maxMainImageBytes {
		return fmt.Errorf("image exceeds %d MB", maxMainImageBytes>>20)
	}
	return nil
}

func isJDImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "jd.com" || strings.HasSuffix(host, ".jd.com") || host == "360buyimg.com" || strings.HasSuffix(host, ".360buyimg.com") || host == "jdcdnimg.com" || strings.HasSuffix(host, ".jdcdnimg.com")
}

func mainImageArchiveFilename(projectName string, now time.Time) string {
	return fmt.Sprintf("%s_主图_%s.zip", safeDownloadName(projectName), now.Format("20060102_150405"))
}

func mainImageFolderName(projectName string) string {
	return safeDownloadName(projectName) + "_主图"
}

func mainImageEntryName(projectName string, index int, image MainImage, imageURL, contentType string) string {
	name := fmt.Sprintf("%03d_%s_%s_%s%s", index+1, safeDownloadName(image.SeriesLabel), safeDownloadName(image.VariantLabel), safeDownloadName(image.SKU), imageFileExtension(imageURL, contentType))
	return mainImageFolderName(projectName) + "/" + name
}

func safeDownloadName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\\/:*?"<>|：／＼＊？＂＜＞｜`, r) {
			return -1
		}
		return r
	}, value)
	if value == "" || value == "." || value == ".." {
		return "京东商品"
	}
	return value
}

func imageFileExtension(rawURL, contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		extension := strings.ToLower(path.Ext(parsed.Path))
		if extension == ".jpg" || extension == ".jpeg" || extension == ".png" || extension == ".webp" || extension == ".avif" || extension == ".gif" {
			return extension
		}
	}
	return ".jpg"
}

func exportFilename(projectName string, now time.Time) string {
	name := strings.TrimSpace(projectName)
	if name == "" {
		name = "京东商品"
	}
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\\/:*?"<>|：／＼＊？＂＜＞｜`, r) {
			return -1
		}
		return r
	}, name)
	if name == "" {
		name = "京东商品"
	}
	return fmt.Sprintf("%s_%s.xlsx", name, now.Format("20060102_150405"))
}
func (h *HTTPHandler) createAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CodeChallenge string `json:"code_challenge"`
		RedirectURI   string `json:"redirect_uri"`
	}
	if err := decode(r, &in); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	u, _ := current(r)
	code, err := h.extensions.CreateAuthorizationCode(r.Context(), u.ID, in.CodeChallenge, in.RedirectURI)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid authorization request"})
		return
	}
	respond(w, 201, map[string]string{"code": code, "redirect_uri": strings.TrimSpace(in.RedirectURI)})
}
