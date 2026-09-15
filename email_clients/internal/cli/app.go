package cli

import (
	"fmt"
	"os"
	"time"

	"email_clients/clients/sweego"
)

func Main(args []string) int {
	if len(args) < 1 {
		printUsage(os.Stderr)
		return 2
	}

	cfg, err := loadConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 2
	}

	client := sweego.NewClient(cfg.BaseURL, cfg.Token, 15*time.Second)

	switch args[0] {
	case "send":
		if err := runSend(args[1:], client, cfg.Provider); err != nil {
			fmt.Fprintln(os.Stderr, "send error:", err)
			return 1
		}
	case "logs":
		if err := runLogs(args[1:], client); err != nil {
			fmt.Fprintln(os.Stderr, "logs error:", err)
			return 1
		}
	case "verify":
		if err := runVerify(args[1:], client); err != nil {
			fmt.Fprintln(os.Stderr, "verify error:", err)
			return 1
		}
	case "bulk-send":
		if err := runBulkSend(args[1:], client, cfg.Provider); err != nil {
			fmt.Fprintln(os.Stderr, "bulk-send error:", err)
			return 1
		}
	case "bulk-send-json", "bulk-send-template":
		if err := runBulkSendDocument(args[1:], client, cfg.Provider); err != nil {
			fmt.Fprintln(os.Stderr, "bulk-send-json error:", err)
			return 1
		}
	case "template-upload":
		clientUUID, err := requireClientUUID(cfg)
		if err == nil {
			err = runTemplateUpload(args[1:], client, clientUUID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "template-upload error:", err)
			return 1
		}
	case "template-update":
		clientUUID, err := requireClientUUID(cfg)
		if err == nil {
			err = runTemplateUpdate(args[1:], client, clientUUID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "template-update error:", err)
			return 1
		}
	case "template-delete":
		clientUUID, err := requireClientUUID(cfg)
		if err == nil {
			err = runTemplateDelete(args[1:], client, clientUUID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "template-delete error:", err)
			return 1
		}
	default:
		printUsage(os.Stderr)
		return 2
	}
	return 0
}
