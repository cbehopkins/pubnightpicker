package cli

import (
	"errors"
	"os"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.sweego.io"
const defaultVerifyTolerance = 5 * time.Minute

type config struct {
	Token      string
	Provider   string
	BaseURL    string
	ClientUUID string
}

func requireClientUUID(cfg config) (string, error) {
	if cfg.ClientUUID == "" {
		return "", errors.New("SWEEGO_CLIENT_UUID is required for template commands")
	}
	return cfg.ClientUUID, nil
}

func loadConfigFromEnv() (config, error) {
	token := strings.TrimSpace(os.Getenv("SWEEGO_TOKEN"))
	if token == "" {
		return config{}, errors.New("SWEEGO_TOKEN is required")
	}
	provider := strings.TrimSpace(os.Getenv("SWEEGO_PROVIDER"))
	if provider == "" {
		return config{}, errors.New("SWEEGO_PROVIDER is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("SWEEGO_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return config{Token: token, Provider: provider, BaseURL: strings.TrimRight(baseURL, "/"), ClientUUID: strings.TrimSpace(os.Getenv("SWEEGO_CLIENT_UUID"))}, nil
}
