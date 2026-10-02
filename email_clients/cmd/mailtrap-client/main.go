package main

import (
	"os"

	"email_clients/internal/mailtrapcli"
)

func main() {
	os.Exit(mailtrapcli.Main(os.Args[1:]))
}
