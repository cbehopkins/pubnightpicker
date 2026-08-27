package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"last_orders/internal/lastorders/app"
)

func main() {
	var (
		dbPath          = flag.String("db-path", "./last-orders.db", "path to SQLite database")
		runFor          = flag.Duration("run-for", 20*time.Second, "how long to run before graceful stop (0 means until signal)")
		pollDelay       = flag.Duration("cellar-poll-delay", 60*time.Millisecond, "delay between claim attempts")
		reevaluateEvery = flag.Duration("event-reevaluate-every", 24*time.Hour, "initial schedule interval for the durable event-venue re-evaluation timer (has no effect once the timer already exists; see docs/adr/0014)")
		httpAddr        = flag.String("http-addr", ":8080", "address to serve HTTP endpoints on (empty disables HTTP)")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *runFor > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, *runFor)
		defer timeoutCancel()
	}

	enableFirestore := os.Getenv("FIRESTORE_EMULATOR_HOST") != ""
	firestoreProjectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if firestoreProjectID == "" {
		firestoreProjectID = "last-orders-emulator"
	}
	if enableFirestore {
		logger.Info("firestore emulator enabled", "firestore_emulator_host", os.Getenv("FIRESTORE_EMULATOR_HOST"), "project_id", firestoreProjectID)
	}

	application, err := app.New(app.Config{
		DBPath:               *dbPath,
		PollDelay:            *pollDelay,
		Logger:               logger,
		EnableFirestore:      enableFirestore,
		FirestoreProjectID:   firestoreProjectID,
		EventReevaluateEvery: *reevaluateEvery,
		HTTPAddr:             *httpAddr,
	})
	if err != nil {
		fatalf("initialise app: %v", err)
	}
	defer application.Close()

	logger.Info("last-orders starting", "db_path", *dbPath)
	if addr := application.HTTPAddr(); addr != "" {
		logger.Info("http endpoints listening", "addr", addr)
	}
	if err := application.Run(ctx); err != nil {
		fatalf("run app: %v", err)
	}

	logger.Info("last-orders stopped")
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
