package main

import (
	"os"

	"email_clients/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
