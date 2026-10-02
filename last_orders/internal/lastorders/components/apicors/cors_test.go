package apicors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS(t *testing.T) {
	for _, test := range []struct {
		name, method, origin, requestedMethod, requestedHeaders string
		status                                                  int
		called                                                  bool
	}{
		{name: "allowed request", method: "POST", origin: "http://localhost:3000", status: 401, called: true},
		{name: "non-browser", method: "POST", status: 401, called: true},
		{name: "allowed preflight", method: "OPTIONS", origin: "http://localhost:3000", requestedMethod: "POST", requestedHeaders: "authorization, content-type", status: 204},
		{name: "denied origin", method: "POST", origin: "https://evil.example", status: 403},
		{name: "denied preflight", method: "OPTIONS", origin: "https://evil.example", requestedMethod: "POST", status: 403},
		{name: "denied method", method: "OPTIONS", origin: "http://localhost:3000", requestedMethod: "DELETE", status: 403},
		{name: "denied header", method: "OPTIONS", origin: "http://localhost:3000", requestedMethod: "POST", requestedHeaders: "X-User-UID", status: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler, err := Wrap([]string{"http://localhost:3000"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusUnauthorized)
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(test.method, "/api/ping", nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Access-Control-Request-Method", test.requestedMethod)
			request.Header.Set("Access-Control-Request-Headers", test.requestedHeaders)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || called != test.called {
				t.Fatalf("status=%d called=%v", response.Code, called)
			}
			wantOrigin := ""
			if test.origin == "http://localhost:3000" {
				wantOrigin = test.origin
			}
			if response.Header().Get("Access-Control-Allow-Origin") != wantOrigin || response.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("unexpected CORS permissions")
			}
		})
	}
}

func TestInvalidOrigins(t *testing.T) {
	for _, origin := range []string{"*", "null", "http://public.example", "https://example.com/path", "https://user@example.com", "https://example.com?query=1"} {
		if _, err := Wrap([]string{origin}, http.NotFoundHandler()); err == nil {
			t.Fatalf("accepted invalid origin %q", origin)
		}
	}
}

func TestPreviewOrigins(t *testing.T) {
	preview := "https://pubnightpicker--pr141-bug-test-build-ie5jywxw.web.app"
	for _, test := range []struct {
		name, origin string
		allowed      bool
	}{
		{name: "preview", origin: preview, allowed: true},
		{name: "other configured site", origin: "https://second-site--test-123.web.app", allowed: true},
		{name: "permanent domain", origin: "https://ampubnight.org", allowed: true},
		{name: "permanent firebase", origin: "https://pubnightpicker.web.app", allowed: true},
		{name: "unrelated site", origin: "https://other-project--pr141.web.app"},
		{name: "prefixed site", origin: "https://evilpubnightpicker--pr141.web.app"},
		{name: "suffix attack", origin: preview + ".evil.example"},
		{name: "extra subdomain", origin: "https://pubnightpicker--pr141.evil.web.app"},
		{name: "http", origin: strings.Replace(preview, "https:", "http:", 1)},
		{name: "custom port", origin: preview + ":8443"},
		{name: "explicit default port", origin: preview + ":443"},
		{name: "path", origin: preview + "/path"},
		{name: "trailing slash", origin: preview + "/"},
		{name: "query", origin: preview + "?key=value"},
		{name: "empty query", origin: preview + "?"},
		{name: "fragment", origin: preview + "#fragment"},
		{name: "userinfo", origin: strings.Replace(preview, "https://", "https://user@", 1)},
		{name: "encoded host", origin: "https://pubnightpicker--preview%2Eweb.app"},
		{name: "empty channel", origin: "https://pubnightpicker--.web.app"},
		{name: "invalid label", origin: "https://pubnightpicker--preview_name.web.app"},
		{name: "oversized label", origin: "https://pubnightpicker--" + strings.Repeat("a", 64) + ".web.app"},
		{name: "null", origin: "null"},
		{name: "multiple origins", origin: preview + " https://evil.example"},
	} {
		for _, method := range []string{"POST", "OPTIONS"} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				called := false
				handler, err := Wrap([]string{"https://ampubnight.org", "https://pubnightpicker.web.app"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusUnauthorized)
				}), "pubnightpicker", "second-site")
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(method, "/api/ping", nil)
				request.Header.Set("Origin", test.origin)
				request.Header.Set("Access-Control-Request-Method", "POST")
				request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				status := http.StatusForbidden
				if test.allowed {
					if method == "OPTIONS" {
						status = http.StatusNoContent
					} else {
						status = http.StatusUnauthorized
					}
				}
				if response.Code != status || called != (test.allowed && method == "POST") {
					t.Fatalf("status=%d called=%v", response.Code, called)
				}
				wantOrigin := ""
				if test.allowed {
					wantOrigin = test.origin
				}
				if response.Header().Get("Access-Control-Allow-Origin") != wantOrigin || response.Header().Get("Access-Control-Allow-Credentials") != "" {
					t.Fatal("unexpected CORS permissions")
				}
			})
		}
	}
}

func TestPreviewOriginsRequireOptIn(t *testing.T) {
	handler, err := Wrap(nil, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/ping", nil)
	request.Header.Set("Origin", "https://pubnightpicker--preview-123.web.app")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestInvalidPreviewSites(t *testing.T) {
	for _, site := range []string{"", "*", "pubnightpicker--*.web.app", "https://pubnightpicker.web.app", "pubnightpicker.web.app", "PubNightPicker", "-pubnightpicker", "pubnightpicker-", strings.Repeat("a", 64)} {
		if _, err := Wrap(nil, http.NotFoundHandler(), site); err == nil {
			t.Fatalf("accepted invalid preview site %q", site)
		}
	}
}
