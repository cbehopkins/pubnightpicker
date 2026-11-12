package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/debug"
	"testing"
)

func TestEndpoint(t *testing.T) {
	previousTag, previousCommit := Tag, Commit
	t.Cleanup(func() { Tag, Commit = previousTag, previousCommit })
	Tag, Commit = "release-v1.2.3", "0123456789abcdef0123456789abcdef01234567"
	response := httptest.NewRecorder()
	Endpoint{}.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	var result struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Tag != Tag || result.Commit != Commit {
		t.Fatalf("status=%d result=%+v", response.Code, result)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected headers: %v", response.Header())
	}
}

func TestBuildIdentity(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		{Key: "vcs.modified", Value: "true"},
	}}
	development := buildIdentity("", "", "development", info)
	if development.Commit != info.Settings[0].Value || development.Modified == nil || !*development.Modified || development.Build != "development" {
		t.Fatalf("development identity = %+v", development)
	}
	release := buildIdentity("release-v1.2.3", "release-commit", "release", info)
	if release.Commit != "release-commit" || release.Modified != nil || release.Build != "release" {
		t.Fatalf("release identity = %+v", release)
	}
	missing := buildIdentity("", "", "development", nil)
	if missing.Commit != "" || missing.Modified != nil {
		t.Fatalf("missing identity = %+v", missing)
	}
}

func TestBuildIdentityLinkFlags(t *testing.T) {
	expectedTag := os.Getenv("TEST_BUILD_TAG")
	expectedCommit := os.Getenv("TEST_BUILD_COMMIT")
	if expectedTag == "" || expectedCommit == "" {
		t.Skip("link flag test values not supplied")
	}
	if Tag != expectedTag || Commit != expectedCommit {
		t.Fatalf("tag=%q commit=%q", Tag, Commit)
	}
}
