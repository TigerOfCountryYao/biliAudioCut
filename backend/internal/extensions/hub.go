package extensions

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Hub struct {
	service *Service
	mu      sync.Mutex
	clients map[uuid.UUID]*websocket.Conn
}

const disconnectGracePeriod = 10 * time.Second

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func NewHub(service *Service) *Hub {
	return &Hub{service: service, clients: map[uuid.UUID]*websocket.Conn{}}
}

// Disconnect removes the current WebSocket for a device. The old connection
// cannot receive another capture task after a new device token is issued or
// the extension is explicitly disconnected.
func (h *Hub) Disconnect(deviceID uuid.UUID) {
	h.mu.Lock()
	conn := h.clients[deviceID]
	delete(h.clients, deviceID)
	h.mu.Unlock()

	if conn == nil {
		return
	}
	_ = conn.Close()
	go h.recoverDisconnectedTasks(deviceID, conn)
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var auth struct {
		Type    string `json:"type"`
		Token   string `json:"token"`
		BuildID string `json:"build_id"`
	}
	if err := conn.ReadJSON(&auth); err != nil || auth.Type != "authenticate" {
		return
	}
	device, err := h.service.Authenticate(r.Context(), auth.Token)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "code": "unauthorized"})
		return
	}
	h.mu.Lock()
	if old := h.clients[device.ID]; old != nil {
		_ = old.Close()
	}
	h.clients[device.ID] = conn
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.clients[device.ID] == conn {
			delete(h.clients, device.ID)
			go h.recoverDisconnectedTasks(device.ID, conn)
		}
		h.mu.Unlock()
	}()
	_ = h.service.Touch(r.Context(), device.ID, auth.BuildID)
	_ = conn.WriteJSON(map[string]string{"type": "authenticated"})
	_ = h.service.StartPendingCaptures(r.Context(), device.UserID)
	h.Dispatch(context.Background(), device.ID)
	conn.SetReadDeadline(time.Time{})
	for {
		var message struct {
			Type string `json:"type"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			return
		}
		if message.Type == "heartbeat" {
			_ = h.service.Touch(context.Background(), device.ID, "")
		}
	}
}

func (h *Hub) recoverDisconnectedTasks(deviceID uuid.UUID, disconnected *websocket.Conn) {
	time.Sleep(disconnectGracePeriod)
	h.mu.Lock()
	connected := h.clients[deviceID]
	h.mu.Unlock()
	if connected != nil && connected != disconnected {
		return
	}
	if err := h.service.RequeueDispatchedTasks(context.Background(), deviceID); err != nil {
		slog.Error("recover dispatched tasks", "error", err)
	}
}
func (h *Hub) Dispatch(ctx context.Context, id uuid.UUID) {
	h.mu.Lock()
	conn := h.clients[id]
	h.mu.Unlock()
	if conn == nil {
		return
	}
	task, err := h.service.ClaimNextTask(ctx, id)
	if err != nil {
		slog.Error("claim task", "error", err)
		return
	}
	if task == nil {
		return
	}
	if err := conn.WriteJSON(map[string]any{"type": "capture", "version": 1, "task_id": task.ID.String(), "source_url": task.SourceURL, "capture_all_skus": task.CaptureAllSKUs}); err != nil {
		slog.Warn("dispatch task", "error", err)
	}
}
func (h *Hub) DispatchForUser(ctx context.Context, userID uuid.UUID) {
	var id uuid.UUID
	if err := h.service.pool.QueryRow(ctx, `SELECT id FROM browser_extensions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&id); err == nil {
		h.Dispatch(ctx, id)
	}
}
