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
	"last_orders/internal/lastorders/components/counter"
	"last_orders/internal/lastorders/components/firebaseidempotency"
)

func main() {
	var (
		dbPath                = flag.String("db-path", "./last-orders.db", "path to SQLite database")
		runFor                = flag.Duration("run-for", 20*time.Second, "how long to run before graceful stop (0 means until signal)")
		pollDelay             = flag.Duration("cellar-poll-delay", 60*time.Millisecond, "delay between claim attempts")
		enableExampleListener = flag.Bool("example-listener", true, "start the trivial example listener")
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

	application, err := app.New(app.Config{
		DBPath:                *dbPath,
		PollDelay:             *pollDelay,
		Logger:                logger,
		IdempotencyRemote:     firebaseidempotency.NewMemoryRemote(true),
		EnableExampleListener: *enableExampleListener,
	})
	if err != nil {
		fatalf("initialise app: %v", err)
	}
	defer application.Close()

	logger.Info("last-orders starting", "db_path", *dbPath, "example_listener", *enableExampleListener)
	if err := application.Run(ctx); err != nil {
		fatalf("run app: %v", err)
	}

	value, err := application.CounterValue(context.Background(), counter.DefaultCounter)
	if err != nil {
		fatalf("read counter value: %v", err)
	}
	logger.Info("last-orders stopped", "counter", counter.DefaultCounter, "value", value)
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
