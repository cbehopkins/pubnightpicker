package apicors

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"last_orders/internal/lastorders/components/firebaseauth"
)

var hostnameLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Wrap(origins []string, next http.Handler, previewSites ...string) (http.Handler, error) {
	for _, site := range previewSites {
		if !hostnameLabel.MatchString(site) {
			return nil, fmt.Errorf("invalid Firebase preview site %q: expected a lowercase DNS label, not a URL or wildcard", site)
		}
	}
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid API origin %q: expected an exact HTTPS origin or local HTTP origin", origin)
		}
		address := net.ParseIP(parsed.Hostname())
		loopback := parsed.Hostname() == "localhost" || (address != nil && address.IsLoopback())
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
			return nil, fmt.Errorf("API origin %q must use HTTPS outside loopback development", origin)
		}
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !allowed[origin] && !allowsPreview(origin, previewSites) {
			firebaseauth.WriteError(w, http.StatusForbidden, "forbidden_origin", "This frontend origin is not allowed.")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			if r.Header.Get("Access-Control-Request-Method") != http.MethodPost {
				firebaseauth.WriteError(w, http.StatusForbidden, "forbidden_preflight", "This API method is not allowed.")
				return
			}
			for _, name := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				name = strings.ToLower(strings.TrimSpace(name))
				if name != "" && name != "authorization" && name != "content-type" {
					firebaseauth.WriteError(w, http.StatusForbidden, "forbidden_preflight", "This API header is not allowed.")
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "POST")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

func allowsPreview(origin string, sites []string) bool {
	if len(sites) == 0 {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || origin != "https://"+parsed.Hostname() {
		return false
	}
	hostname := parsed.Hostname()
	if !strings.HasSuffix(hostname, ".web.app") {
		return false
	}
	label := strings.TrimSuffix(hostname, ".web.app")
	if !hostnameLabel.MatchString(label) {
		return false
	}
	for _, site := range sites {
		if strings.HasPrefix(label, site+"--") {
			return true
		}
	}
	return false
}
