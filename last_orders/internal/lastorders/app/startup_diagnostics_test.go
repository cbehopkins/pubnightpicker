package app

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (transport diagnosticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestDiagnoseFirestoreHTTPS(t *testing.T) {
	for _, contentType := range []string{"application/json", "text/html; charset=UTF-8"} {
		t.Run(contentType, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			calls := 0
			transport := diagnosticTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodGet || request.URL.Host != "firestore.googleapis.com" ||
					request.URL.Path != "/v1/projects/test-project/databases/(default)/documents/listener_state/last_orders" {
					t.Fatalf("unexpected diagnostic request: %s %s", request.Method, request.URL)
				}
				if len(request.Header) != 0 {
					t.Fatalf("unexpected request headers: %v", request.Header)
				}
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("diagnostic request must have a five-second deadline")
				}
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header: http.Header{
						"Content-Type": {contentType},
						"Location":     {"https://example.com/private-redirect"},
					},
					Proto: "HTTP/2.0",
					TLS:   &tls.ConnectionState{NegotiatedProtocol: "h2"},
					Body:  io.NopCloser(strings.NewReader("private-response-body")),
				}, nil
			})
			diagnoseFirestoreHTTPS(logger, "test-project", transport)
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["status"] != float64(http.StatusForbidden) || record["content_type"] != contentType ||
				record["protocol"] != "HTTP/2.0" || record["tls_alpn"] != "h2" || record["authenticated"] != false {
				t.Fatalf("unexpected diagnostic: %s", output.String())
			}
			if calls != 1 || strings.Contains(output.String(), "private-") {
				t.Fatalf("diagnostic must not log response bodies or extra headers: %s", output.String())
			}
		})
	}
}

func TestDiagnoseFirestoreHTTPSDoesNotFollowRedirects(t *testing.T) {
	var output bytes.Buffer
	calls := 0
	transport := diagnosticTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {"https://example.com/private-redirect"}},
			Body:       http.NoBody,
		}, nil
	})
	diagnoseFirestoreHTTPS(slog.New(slog.NewJSONHandler(&output, nil)), "test-project", transport)
	if calls != 1 || !strings.Contains(output.String(), `"status":302`) {
		t.Fatalf("diagnostic followed redirect or failed to log status: %s", output.String())
	}
}

func TestDiagnoseFirestoreHTTPSRequestFailure(t *testing.T) {
	var output bytes.Buffer
	transport := diagnosticTransport(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("private-proxy-credentials")
	})
	diagnoseFirestoreHTTPS(slog.New(slog.NewJSONHandler(&output, nil)), "test-project", transport)
	if !strings.Contains(output.String(), `"outcome":"request_failed"`) || strings.Contains(output.String(), "private-") {
		t.Fatalf("unexpected failure diagnostic: %s", output.String())
	}
}

func TestDiagnoseFirestoreHTTPSDeadline(t *testing.T) {
	var output bytes.Buffer
	transport := diagnosticTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	diagnoseFirestoreHTTPS(slog.New(slog.NewJSONHandler(&output, nil)), "test-project", transport)
	if !strings.Contains(output.String(), `"deadline_exceeded":true`) {
		t.Fatalf("expected bounded diagnostic: %s", output.String())
	}
}
