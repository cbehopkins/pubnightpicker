package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/mail"
	"os"

	"email_clients/clients/sweego"
	"email_clients/clients/sweego/logs"
)

func parseAddress(raw string) (sweego.EmailAddress, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return sweego.EmailAddress{}, err
	}
	return sweego.EmailAddress{Email: addr.Address, Name: addr.Name}, nil
}

func parseFlagsWithPositionals(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}

func printHTTPResult(status int, body []byte) {
	fmt.Printf("HTTP status: %d\n", status)
	fmt.Println(prettyJSON(body))
}

func printVerificationResult(result logs.VerificationResult) {
	fmt.Printf("Verification: %s\n", result.Status)
	switch result.Status {
	case logs.VerificationFound:
		fmt.Printf("  correlation_id: %s\n  recipient:      %s\n  transaction_id: %s\n  swg_uid:        %s\n  email_status:   %s\n", result.CorrelationID, result.Recipient, result.TransactionID, result.SwgUID, result.EmailStatus)
	case logs.VerificationNotFound:
		fmt.Printf("  correlation_id: %s\n  recipient:      %s\n", result.CorrelationID, result.Recipient)
	case logs.VerificationQueryError:
		fmt.Printf("  error: %v\n", result.Err)
	}
}

func prettyJSON(raw []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err == nil {
		return out.String()
	}
	return string(raw)
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  sweego_client send --from <email> --to <email> --subject <text> --text <text> [--message-id <id>] [--verify-tolerance <duration>] [--dry-run]")
	fmt.Fprintln(out, "  sweego_client logs [--uid <value>] [--to <email>] [--date <YYYY-MM-DD>]")
	fmt.Fprintln(out, "  sweego_client verify --to <email> --message-id <id> [--sent-at <RFC3339>] [--tolerance <duration>]")
	fmt.Fprintln(out, "  sweego_client bulk-send --from <email> --to <email,email,...> [--subject <text>] [--text <text>] [--discard-response] [--dry-run]")
	fmt.Fprintln(out, "  sweego_client bulk-send-json <targets.json> [--from <email>] [--message-id <id>] [--discard-response] [--dry-run]")
	fmt.Fprintln(out, "  sweego_client template-upload <template-file> [--name <text>]")
	fmt.Fprintln(out, "  sweego_client template-update <template-uuid> <template-file> [--name <text>]")
	fmt.Fprintln(out, "  sweego_client template-delete <template-uuid>")
	fmt.Fprintln(out, "\nEnvironment:")
	fmt.Fprintln(out, "  SWEEGO_TOKEN (required)")
	fmt.Fprintln(out, "  SWEEGO_PROVIDER (required)")
	fmt.Fprintln(out, "  SWEEGO_CLIENT_UUID (required by the template commands)")
	fmt.Fprintln(out, "  SWEEGO_BASE_URL (optional, defaults to https://api.sweego.io)")
}
