package projects

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
	mux.Handle("PUT /api/projects/{projectId}/sku-selection", protect(http.HandlerFunc(h.selection)))
	mux.Handle("POST /api/projects/{projectId}/retry", protect(http.HandlerFunc(h.retry)))
	mux.Handle("GET /api/projects/{projectId}/export.xlsx", protect(http.HandlerFunc(h.export)))
	mux.Handle("POST /api/extension/authorization-codes", protect(http.HandlerFunc(h.createAuthorizationCode)))
	mux.HandleFunc("POST /api/extension/token", h.exchangeToken)
	mux.HandleFunc("DELETE /api/extension/device", h.disconnectDevice)
	mux.HandleFunc("POST /api/extension/capture-results", h.captureResult)
	mux.HandleFunc("POST /api/extension/capture-failures", h.captureFailure)
	mux.Handle("GET /api/extension/authorize", protect(http.HandlerFunc(h.authorizeRedirect)))
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
	respond(w, 201, map[string]string{"access_token": device.Token, "token_type": "Bearer"})
}

func (h *HTTPHandler) disconnectDevice(w http.ResponseWriter, r *http.Request) {
	if err := h.extensions.Disconnect(r.Context(), extensionToken(r)); err != nil {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
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
		TaskID uuid.UUID `json:"task_id"`
		Code   string    `json:"code"`
		Detail string    `json:"detail"`
	}
	if err := decode(r, &in); err != nil || in.TaskID == uuid.Nil || strings.TrimSpace(in.Code) == "" {
		respond(w, 400, map[string]string{"error": "invalid failure"})
		return
	}
	if err := h.extensions.FailTask(r.Context(), in.TaskID, device.ID, in.Code, in.Detail); err != nil {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
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
		Name  string   `json:"name"`
		Links []string `json:"links"`
	}
	if err := decode(r, &in); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	u, _ := current(r)
	p, err := h.service.Create(r.Context(), u.ID, in.Name, in.Links)
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
	a, b, c, err := h.service.RawExportRows(r.Context(), id, u.ID, isAdmin(u))
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
	file, err := production.BuildCaptureWorkbook(a, b, c)
	if err != nil {
		respond(w, 500, map[string]string{"error": "export failed"})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=jd-capture.xlsx")
	_, _ = w.Write(file)
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
