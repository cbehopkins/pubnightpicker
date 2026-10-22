package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
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
			logger := slog.New(slog.NewJSONHandler(&output, nil)).With("network", "tcp4")
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
				record["protocol"] != "HTTP/2.0" || record["tls_alpn"] != "h2" || record["authenticated"] != false ||
				record["network"] != "tcp4" {
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

func TestFirestoreDiagnosticTransportAddressFamily(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	for _, network := range []string{"tcp", "tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			transport := firestoreDiagnosticTransport(network)
			defer transport.CloseIdleConnections()
			if !transport.ForceAttemptHTTP2 || transport.TLSClientConfig != nil {
				t.Fatal("diagnostic must preserve HTTP/2 support and default TLS verification")
			}
			client := &http.Client{Transport: transport, Timeout: time.Second}
			response, err := client.Get(server.URL)
			if err == nil {
				response.Body.Close()
			}
			if network == "tcp6" {
				if err == nil {
					t.Fatal("IPv6-only diagnostic must not connect to an IPv4-only listener")
				}
				return
			}
			if err != nil {
				t.Fatalf("diagnostic failed to reach IPv4 listener: %v", err)
			}
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", response.StatusCode)
			}
		})
	}
}

func TestDialFirestoreIPv4(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("IPv4", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		connection, err := dialFirestoreIPv4(ctx, listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		peer, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer peer.Close()
		if address := connection.RemoteAddr().(*net.TCPAddr); address.IP.To4() == nil {
			t.Fatalf("expected IPv4 peer, got %s", address)
		}
	})
	t.Run("IPv6 rejected", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		connection, err := dialFirestoreIPv4(ctx, net.JoinHostPort("::1", port))
		if err == nil {
			connection.Close()
			t.Fatal("IPv4 dialer must reject an IPv6 address")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		connection, err := dialFirestoreIPv4(ctx, listener.Addr().String())
		if err == nil {
			connection.Close()
			t.Fatal("dialer must honour cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	})
}
