package version

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
)

var Tag string
var Commit string
var Build = "development"

type Endpoint struct{}

type identity struct {
	Tag      string `json:"tag"`
	Commit   string `json:"commit"`
	Build    string `json:"build"`
	Modified *bool  `json:"modified,omitempty"`
}

func buildIdentity(tag, commit, build string, info *debug.BuildInfo) identity {
	result := identity{Tag: tag, Commit: commit, Build: build}
	if build != "development" || info == nil {
		return result
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if result.Commit == "" {
				result.Commit = setting.Value
			}
		case "vcs.modified":
			if setting.Value == "true" || setting.Value == "false" {
				modified := setting.Value == "true"
				result.Modified = &modified
			}
		}
	}
	return result
}

func (Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	info, _ := debug.ReadBuildInfo()
	_ = json.NewEncoder(w).Encode(buildIdentity(Tag, Commit, Build, info))
}
