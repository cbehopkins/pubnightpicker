package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cellar/pkg/cellar"
	publicsqlite "cellar/pkg/sqlite"
	"last_orders/internal/proto/app"
	"last_orders/internal/proto/firebase"
	"last_orders/internal/proto/idempotency"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
)

func main() {
	var (
		phase  = flag.Int("phase", 4, "prototype phase: 1=firebase_observe, 2=housekeeping_to_cellar, 3=handler_schedules_future, 4=firebase_to_cellar, 5=firebase_to_cellar_with_idempotency")
		runFor = flag.Duration("run-for", 45*time.Second, "how long to run before graceful stop")

		housekeepingEvery = flag.Duration("housekeeping-every", 5*time.Second, "housekeeping enqueue period")
		housekeepingLead  = flag.Duration("housekeeping-lead", 2*time.Second, "delay between enqueue and due time")
		chainDelay        = flag.Duration("chain-delay", 10*time.Second, "delay for chained cell scheduling")
		pollDelay         = flag.Duration("cellar-poll-delay", 250*time.Millisecond, "cell claim polling delay")
		sqlitePath        = flag.String("sqlite-path", "", "optional sqlite path for durable cellar store; default is in-memory")

		enableFirebase   = flag.Bool("firebase", false, "enable firebase listeners")
		firebaseProject  = flag.String("firebase-project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "firebase/google cloud project id")
		firebaseCredPath = flag.String("firebase-credentials", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"), "path to service account json")
		pollCollection   = flag.String("poll-collection", "polls", "firestore collection containing poll docs")
		minDate          = flag.String("min-date", "", "optional min poll date filter (string compare)")
		emitInitial      = flag.Bool("emit-initial", false, "emit events from the first firestore snapshot")

		idempotencyMode   = flag.String("idempotency", "none", "none|memory|firestore")
		enableFailureCell = flag.Bool("failure-cell", false, "enqueue a fail-once handler cell on startup")
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

	var source firebase.Source
	var firestoreClient *firestore.Client
	var store appStoreCloser
	var err error

	if *sqlitePath != "" {
		store, err = publicsqlite.Open(*sqlitePath, nil)
		if err != nil {
			fatalf("sqlite store init failed: %v", err)
		}
		defer store.Close()
	}

	if *enableFirebase {
		if *firebaseProject == "" {
			fatalf("-firebase-project or GOOGLE_CLOUD_PROJECT is required when -firebase=true")
		}
		firestoreClient, err = buildFirestoreClient(ctx, *firebaseProject, *firebaseCredPath)
		if err != nil {
			fatalf("firestore client init failed: %v", err)
		}
		defer firestoreClient.Close()

		source = firebase.NewFirestoreSource(firestoreClient, firebase.FirestoreSourceConfig{
			PollCollection: *pollCollection,
			MinDate:        *minDate,
			EmitInitial:    *emitInitial,
		}, logger)
	}

	deduper := idempotency.Deduper(idempotency.NoopDeduper{})
	switch strings.ToLower(*idempotencyMode) {
	case "none":
		deduper = idempotency.NoopDeduper{}
	case "memory":
		deduper = idempotency.NewMemoryDeduper()
	case "firestore":
		if firestoreClient == nil {
			fatalf("-idempotency=firestore requires -firebase=true")
		}
		deduper = idempotency.NewFirestoreDeduper(firestoreClient, "listener_state", "last_orders")
	default:
		fatalf("unsupported idempotency mode: %s", *idempotencyMode)
	}

	prototype, err := app.New(app.Config{
		Phase:                    *phase,
		PollDelay:                *pollDelay,
		HousekeepingEvery:        *housekeepingEvery,
		HousekeepingScheduleLead: *housekeepingLead,
		ChainDelay:               *chainDelay,
		EnableFailureCell:        *enableFailureCell,
		Store:                    store,
		EnableFirebase:           *enableFirebase,
		FirebaseSource:           source,
		Deduper:                  deduper,
		Logger:                   logger,
	})
	if err != nil {
		fatalf("prototype init failed: %v", err)
	}

	logger.Info("last_orders prototype starting",
		"phase", *phase,
		"firebase_enabled", *enableFirebase,
		"idempotency", *idempotencyMode,
	)

	if err := prototype.Run(ctx); err != nil {
		fatalf("prototype run failed: %v", err)
	}

	snapshot := prototype.RecorderSnapshot()
	logger.Info("prototype stopped",
		"housekeeping_runs", snapshot.HousekeepingRuns,
		"future_runs", snapshot.FutureRuns,
		"firebase_runs", snapshot.FirebaseRuns,
	)
}

func buildFirestoreClient(ctx context.Context, projectID, credentialsPath string) (*firestore.Client, error) {
	if credentialsPath == "" {
		return firestore.NewClient(ctx, projectID)
	}
	return firestore.NewClient(ctx, projectID, option.WithCredentialsFile(credentialsPath))
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

type appStoreCloser interface {
	cellar.Store
	Close() error
}
