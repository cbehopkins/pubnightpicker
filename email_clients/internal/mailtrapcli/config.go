package mailtrapcli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"email_clients/clients/mailtrap"

	sdk "github.com/mailtrap/mailtrap-go"
)

type config struct {
	Token     string
	Sandbox   bool
	SandboxID int64
}

func loadConfigFromEnv() (config, error) {
	cfg := config{Token: strings.TrimSpace(os.Getenv("MAILTRAP_TOKEN"))}
	if cfg.Token == "" {
		return config{}, errors.New("MAILTRAP_TOKEN is required")
	}
	if value := strings.TrimSpace(os.Getenv("MAILTRAP_USE_SANDBOX")); value != "" {
		var err error
		cfg.Sandbox, err = strconv.ParseBool(value)
		if err != nil {
			return config{}, errors.New("MAILTRAP_USE_SANDBOX must be a boolean")
		}
	}
	if value := strings.TrimSpace(os.Getenv("MAILTRAP_SANDBOX_ID")); value != "" {
		var err error
		cfg.SandboxID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || cfg.SandboxID <= 0 {
			return config{}, errors.New("MAILTRAP_SANDBOX_ID must be a positive integer")
		}
	}
	if cfg.Sandbox && cfg.SandboxID == 0 {
		return config{}, errors.New("MAILTRAP_SANDBOX_ID is required in sandbox mode")
	}
	return cfg, nil
}

func newClient(cfg config) (*mailtrap.Client, error) {
	options := []sdk.Option{
		sdk.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}),
		sdk.WithSandbox(cfg.Sandbox),
	}
	if cfg.SandboxID != 0 {
		options = append(options, sdk.WithSandboxID(cfg.SandboxID))
	}
	client, err := sdk.NewClient(cfg.Token, options...)
	if err != nil {
		return nil, fmt.Errorf("create Mailtrap SDK client: %w", err)
	}
	return mailtrap.NewClient(client)
}
