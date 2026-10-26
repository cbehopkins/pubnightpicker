package landlordinternal

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	durableemail "durable_email"
)

const EmailHistoryPath = "/internal/v1/email-history"

type ProgressReader interface {
	QueryProgress(context.Context, durableemail.ProgressFilter) ([]durableemail.ProgressRow, error)
}

type EmailHistoryHandler struct {
	Store ProgressReader
	Token string
}

type EmailHistoryClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

type historyRequest struct {
	UserID string `json:"user_id"`
}

type historyResponse struct {
	Entries []durableemail.ProgressRow `json:"entries"`
}

func (h EmailHistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if h.Token == "" || !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, "Bearer ")), []byte(h.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var request historyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.UserID) == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	entries, err := h.Store.QueryProgress(ctx, durableemail.ProgressFilter{UserID: request.UserID, LatestFirst: true, Limit: 20})
	if err != nil {
		http.Error(w, "email history unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(historyResponse{Entries: entries})
}

func (c *EmailHistoryClient) QueryProgress(ctx context.Context, filter durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
	if strings.TrimSpace(filter.UserID) == "" {
		return nil, fmt.Errorf("email history query requires a user id")
	}
	if c.Token == "" {
		return nil, fmt.Errorf("Landlord internal API token is required")
	}
	body, err := json.Marshal(historyRequest{UserID: filter.UserID})
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+EmailHistoryPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build Landlord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query Landlord: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Landlord returned %s", response.Status)
	}
	var result historyResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Landlord response: %w", err)
	}
	return result.Entries, nil
}
