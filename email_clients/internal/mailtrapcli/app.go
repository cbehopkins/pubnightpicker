package mailtrapcli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"email_clients/clients/mailtrap"
)

type clientFactory func(config) (*mailtrap.Client, error)

type app struct {
	out       io.Writer
	errOut    io.Writer
	newClient clientFactory
}

func Main(args []string) int {
	return run(args, os.Stdout, os.Stderr, newClient)
}

func run(args []string, out, errOut io.Writer, factory clientFactory) int {
	if len(args) == 0 {
		printUsage(errOut)
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printUsage(out)
		return 0
	}
	application := app{out: out, errOut: errOut, newClient: factory}
	var err error
	switch args[0] {
	case "send":
		err = application.runSend(args[1:])
	case "batch-send-json":
		err = application.runBatchDocument(args[1:])
	case "verify":
		err = application.runVerify(args[1:])
	default:
		fmt.Fprintf(errOut, "unknown command %q\n", args[0])
		printUsage(errOut)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	return 0
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: mailtrap-client <send|batch-send-json|verify> [options]")
}

func (a *app) client() (*mailtrap.Client, config, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, config{}, fmt.Errorf("config: %w", err)
	}
	client, err := a.newClient(cfg)
	return client, cfg, err
}
