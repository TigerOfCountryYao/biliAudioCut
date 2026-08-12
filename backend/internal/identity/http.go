package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/identity/sqlc"
)

const sessionCookieName = "pvs_session"

type sessionAuthenticator interface {
	Login(context.Context, LoginInput) (AuthenticatedSession, error)
	CurrentUser(context.Context, string) (identitysqlc.User, error)
	Logout(context.Context, string) error
}

type HTTPHandler struct {
	sessions     sessionAuthenticator
	cookieSecure bool
}

type contextKey string

const currentUserContextKey contextKey = "current-user"

func (h *HTTPHandler) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})
			return
		}
		user, err := h.sessions.CurrentUser(r.Context(), cookie.Value)
		if errors.Is(err, ErrUnauthenticated) {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), currentUserContextKey, user)))
	})
}

func CurrentUserFromContext(ctx context.Context) (identitysqlc.User, bool) {
	user, ok := ctx.Value(currentUserContextKey).(identitysqlc.User)
	return user, ok
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHTTPHandler(sessions sessionAuthenticator, cookieSecure bool) *HTTPHandler {
	return &HTTPHandler{
		sessions:     sessions,
		cookieSecure: cookieSecure,
	}
}

func (h *HTTPHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/me", h.currentUser)
}

func (h *HTTPHandler) login(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	session, err := h.sessions.Login(r.Context(), LoginInput{
		Email:    input.Email,
		Password: input.Password,
	})
	if errors.Is(err, ErrInvalidCredentials) {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "invalid email or password"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	h.setSessionCookie(w, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, toUserResponse(session.User))
}

func (h *HTTPHandler) currentUser(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})
		return
	}

	user, err := h.sessions.CurrentUser(r.Context(), cookie.Value)
	if errors.Is(err, ErrUnauthenticated) {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (h *HTTPHandler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		if err := h.sessions.Logout(r.Context(), cookie.Value); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
			return
		}
	}

	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *HTTPHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func toUserResponse(user identitysqlc.User) userResponse {
	return userResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Role:        user.Role,
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
