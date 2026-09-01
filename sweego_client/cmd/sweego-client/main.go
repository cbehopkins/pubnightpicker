package main

import (
	"os"

	"sweego_client/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
