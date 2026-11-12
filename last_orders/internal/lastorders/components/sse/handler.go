package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Handler is the endpoint adapter for one complete-view stream. UID is empty
// for a public anonymous view. Authorization returns the ordinary HTTP status
// to use when access is denied (typically 401 or 404).
type Handler struct {
	Hub                *Hub
	Ready              <-chan struct{}
	ViewKey            string
	AuthorizationScope string
	UID                string
	EventName          string
	// HeartbeatInterval controls SSE comment frames. Zero selects the default
	// interval; a negative value is invalid.
	HeartbeatInterval time.Duration
	// WriteTimeout bounds each frame's write and flush. Zero selects the
	// default. The inherited server deadline is cleared for this stream only,
	// then a fresh deadline is applied and cleared around each frame.
	WriteTimeout time.Duration
	Authorize    func(context.Context) (allowed bool, deniedStatus int)
	Snapshot     func(context.Context, CursorInput) (ViewSnapshot, error)
}

// ViewSnapshot is one consistent replacement and its synchronization cursor.
// Cursor must also appear in Data when the endpoint contract includes it.
type ViewSnapshot struct {
	Data     json.RawMessage
	Cursor   string
	Sequence int64
}

// CursorInput is the reconnect cursor supplied by the client, if any. The
// initial snapshot adapter validates it but still returns a full view in V1.
type CursorInput struct {
	Value    string
	Supplied bool
}

const DefaultHeartbeatInterval = 15 * time.Second
const DefaultWriteTimeout = 15 * time.Second

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.valid() {
		http.Error(w, "SSE handler is not configured", http.StatusInternalServerError)
		return
	}
	select {
	case <-h.Ready:
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "stream is starting", http.StatusServiceUnavailable)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming is unsupported", http.StatusInternalServerError)
		return
	}

	// Register before authorization so a racing invalidation is retained.
	sub := h.Hub.Register(h.ViewKey, h.AuthorizationScope, h.UID)
	defer sub.Unregister()
	if h.stopping(sub) {
		return
	}
	if !h.clearWriteDeadline(w) {
		return
	}
	if !h.authorized(r.Context(), w) {
		return
	}
	if h.stopping(sub) {
		return
	}
	if h.invalidated(sub) {
		h.writeReset(w)
		return
	}
	cursor, supplied := RequestCursor(r)
	initial, err := h.Snapshot(r.Context(), CursorInput{Value: cursor, Supplied: supplied})
	if err != nil {
		http.Error(w, "view is unavailable", http.StatusInternalServerError)
		return
	}
	if !validSnapshot(initial) {
		http.Error(w, "view is unavailable", http.StatusInternalServerError)
		return
	}
	if h.stopping(sub) {
		return
	}
	if h.invalidated(sub) {
		h.writeReset(w)
		return
	}
	sub.DiscardChangesThrough(initial.Sequence)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	if !h.writeEvent(w, h.EventName, initial.Data, initial.Cursor) {
		return
	}
	lastSequence := initial.Sequence
	ticker := time.NewTicker(h.heartbeatInterval())
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		case <-sub.Invalidated():
			h.writeReset(w)
			return
		case <-ticker.C:
			if h.stopping(sub) {
				return
			}
			if h.invalidated(sub) {
				h.writeReset(w)
				return
			}
			if !h.writeHeartbeat(w) {
				return
			}
		case <-sub.Changed():
			changedThrough := sub.TakeChanged()
			if changedThrough <= lastSequence {
				continue
			}
			if h.invalidated(sub) {
				h.writeReset(w)
				return
			}
			if !h.authorized(r.Context(), w) {
				return
			}
			view, err := h.Snapshot(r.Context(), CursorInput{})
			if err != nil || !validSnapshot(view) || view.Sequence < changedThrough {
				return
			}
			if h.invalidated(sub) {
				h.writeReset(w)
				return
			}
			if view.Sequence <= lastSequence {
				sub.DiscardChangesThrough(view.Sequence)
				continue
			}
			sub.DiscardChangesThrough(view.Sequence)
			if !h.writeEvent(w, h.EventName, view.Data, view.Cursor) {
				return
			}
			lastSequence = view.Sequence
		}
	}
}

func (h *Handler) valid() bool {
	if h == nil || h.Hub == nil || h.Ready == nil || h.ViewKey == "" || h.AuthorizationScope == "" || h.EventName == "" || h.Authorize == nil || h.Snapshot == nil || h.WriteTimeout < 0 || h.HeartbeatInterval < 0 {
		return false
	}
	return !strings.ContainsAny(h.EventName, "\r\n\x00")
}

func (h *Handler) heartbeatInterval() time.Duration {
	if h.HeartbeatInterval == 0 {
		return DefaultHeartbeatInterval
	}
	return h.HeartbeatInterval
}

func (h *Handler) writeTimeout() time.Duration {
	if h.WriteTimeout == 0 {
		return DefaultWriteTimeout
	}
	return h.WriteTimeout
}

func (h *Handler) authorized(ctx context.Context, w http.ResponseWriter) bool {
	allowed, status := h.Authorize(ctx)
	if allowed {
		return true
	}
	if status < 400 || status > 599 {
		status = http.StatusNotFound
	}
	http.Error(w, http.StatusText(status), status)
	return false
}

func (h *Handler) invalidated(sub *Subscription) bool {
	select {
	case <-sub.Invalidated():
		return true
	default:
		return false
	}
}

func (h *Handler) stopping(sub *Subscription) bool {
	select {
	case <-sub.Done():
		return true
	default:
		return false
	}
}

func (h *Handler) writeReset(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	_ = h.writeEvent(w, "view_reset", json.RawMessage(`{}`), "")
}

func (h *Handler) writeEvent(w http.ResponseWriter, name string, data json.RawMessage, cursor string) bool {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil || strings.ContainsAny(cursor, "\x00\r\n") {
		return false
	}
	id := ""
	if cursor != "" {
		id = "id: " + cursor + "\n"
	}
	return h.writeFrame(w, fmt.Sprintf("%sevent: %s\ndata: %s\n\n", id, name, compact.Bytes()))
}

func (h *Handler) writeHeartbeat(w http.ResponseWriter) bool {
	return h.writeFrame(w, ": keepalive\n\n")
}

func (h *Handler) writeFrame(w http.ResponseWriter, frame string) bool {
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(h.writeTimeout())); err != nil {
		return false
	}
	if _, err := fmt.Fprint(w, frame); err != nil {
		return false
	}
	if err := controller.Flush(); err != nil {
		return false
	}
	// Clear before the deadline expires so the next frame can install a new
	// bound. Go's HTTP ResponseController cannot revive an expired deadline.
	return controller.SetWriteDeadline(time.Time{}) == nil
}

func validSnapshot(snapshot ViewSnapshot) bool {
	return snapshot.Sequence >= 0 && json.Valid(snapshot.Data) && !strings.ContainsAny(snapshot.Cursor, "\x00\r\n")
}

func (h *Handler) clearWriteDeadline(w http.ResponseWriter) bool {
	return http.NewResponseController(w).SetWriteDeadline(time.Time{}) == nil
}
