//go:build e2e

package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const E2EClockControlBuild = true

type E2EClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewE2EClock(initial time.Time) (*E2EClock, error) {
	if initial.IsZero() {
		return nil, errors.New("initial application instant is required")
	}
	return &E2EClock{now: initial}, nil
}

func (clock *E2EClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.now
}

func E2EClockControlHandler(clock *E2EClock, notifyScheduling func()) http.Handler {
	mux := http.NewServeMux()
	if clock == nil || notifyScheduling == nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			writeE2EClockResponse(w, http.StatusServiceUnavailable, e2eClockResponse{Error: "clock control is unavailable"})
		})
		return mux
	}
	mux.HandleFunc("GET /clock", func(w http.ResponseWriter, _ *http.Request) {
		writeE2EClockResponse(w, http.StatusOK, e2eClockResponse{Instant: clock.Now().Format(time.RFC3339Nano)})
	})
	mux.HandleFunc("POST /clock", func(w http.ResponseWriter, r *http.Request) {
		var request e2eClockRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeE2EClockResponse(w, http.StatusBadRequest, e2eClockResponse{Error: "request must contain one instant"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeE2EClockResponse(w, http.StatusBadRequest, e2eClockResponse{Error: "request must contain one instant"})
			return
		}
		requested, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(request.Instant))
		if err != nil {
			writeE2EClockResponse(w, http.StatusBadRequest, e2eClockResponse{Error: "instant must be RFC3339 with an explicit offset"})
			return
		}

		clock.mu.Lock()
		defer clock.mu.Unlock()
		if requested.Before(clock.now) {
			writeE2EClockResponse(w, http.StatusConflict, e2eClockResponse{Error: "application time cannot move backwards"})
			return
		}
		notified := false
		if requested.After(clock.now) {
			clock.now = requested
			notifyScheduling()
			notified = true
		}
		writeE2EClockResponse(w, http.StatusOK, e2eClockResponse{
			Instant:            clock.now.Format(time.RFC3339Nano),
			SchedulingNotified: notified,
		})
	})
	return mux
}

type e2eClockRequest struct {
	Instant string `json:"instant"`
}

type e2eClockResponse struct {
	Instant            string `json:"instant,omitempty"`
	SchedulingNotified bool   `json:"scheduling_notified"`
	Error              string `json:"error,omitempty"`
}

func writeE2EClockResponse(w http.ResponseWriter, status int, response e2eClockResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
