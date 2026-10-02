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

	"cloud.google.com/go/firestore"

	"last_orders/internal/lastorders/app"
	emailplugin "last_orders/internal/lastorders/plugins/email"
	pushplugin "last_orders/internal/lastorders/plugins/push"
)

func main() {
	var (
		dbPath            = flag.String("db-path", "./last-orders.db", "path to SQLite database")
		runFor            = flag.Duration("run-for", 0, "how long to run before graceful stop (0 means until signal)")
		pollDelay         = flag.Duration("cellar-poll-delay", 60*time.Millisecond, "delay between claim attempts")
		reevaluateEvery   = flag.Duration("event-reevaluate-every", 24*time.Hour, "initial schedule interval for the durable event-venue re-evaluation timer (has no effect once the timer already exists; see docs/adr/0014)")
		httpAddr          = flag.String("http-addr", ":8080", "address to serve HTTP endpoints on (empty disables HTTP)")
		allowAuthEmulator = flag.Bool("allow-auth-emulator", false, "allow unsigned Firebase Auth tokens from a loopback emulator (development only)")
		pushClient        = flag.String("push-client", string(pushplugin.ClientDummy), "push delivery client: dummy or webpush")
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

	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	firestoreProjectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	switch {
	case firestoreProjectID != "":
	case emulatorHost != "":
		firestoreProjectID = "last-orders-emulator"
	default:
		// Read project_id from the credentials in GOOGLE_APPLICATION_CREDENTIALS.
		firestoreProjectID = firestore.DetectProjectID
	}
	if emulatorHost != "" {
		logger.Info("firestore emulator enabled", "firestore_emulator_host", emulatorHost, "project_id", firestoreProjectID)
	} else {
		logger.Info("firestore production enabled", "project_id", firestoreProjectID)
	}

	emailOptions := emailplugin.Options{Client: emailplugin.ClientDummy, Logger: logger}
	sweegoToken := strings.TrimSpace(os.Getenv("SWEEGO_TOKEN"))
	sweegoProvider := strings.TrimSpace(os.Getenv("SWEEGO_PROVIDER"))
	if sweegoToken != "" || sweegoProvider != "" {
		emailOptions.Client = emailplugin.ClientSweego
		emailOptions.SweegoToken = sweegoToken
		emailOptions.SweegoProvider = sweegoProvider
		emailOptions.SweegoBaseURL = os.Getenv("SWEEGO_BASE_URL")
		logger.Info("Sweego email client enabled", "provider", sweegoProvider)
	}

	authProjectID := strings.TrimSpace(os.Getenv("FIREBASE_AUTH_PROJECT_ID"))
	if authProjectID == "" {
		authProjectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	var allowedAPIOrigins []string
	if rawOrigins, configured := os.LookupEnv("LAST_ORDERS_ALLOWED_ORIGINS"); configured {
		allowedAPIOrigins = []string{}
		for _, origin := range strings.Split(rawOrigins, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				allowedAPIOrigins = append(allowedAPIOrigins, origin)
			}
		}
	}
	var allowedAPIPreviewSites []string
	for _, site := range strings.Split(os.Getenv("LAST_ORDERS_ALLOWED_PREVIEW_SITES"), ",") {
		if site = strings.TrimSpace(site); site != "" {
			allowedAPIPreviewSites = append(allowedAPIPreviewSites, site)
		}
	}
	if *allowAuthEmulator && os.Getenv("FIREBASE_AUTH_EMULATOR_HOST") != "" {
		logger.Warn("Firebase Auth emulator enabled: unsigned tokens accepted for local development only")
	}
	application, err := app.New(app.Config{
		DBPath:                 *dbPath,
		PollDelay:              *pollDelay,
		Logger:                 logger,
		EnableFirestore:        true,
		FirestoreProjectID:     firestoreProjectID,
		EventReevaluateEvery:   *reevaluateEvery,
		HTTPAddr:               *httpAddr,
		AuthProjectID:          authProjectID,
		AllowAuthEmulator:      *allowAuthEmulator,
		AllowedAPIOrigins:      allowedAPIOrigins,
		AllowedAPIPreviewSites: allowedAPIPreviewSites,
		Email:                  emailOptions,
		Push: pushplugin.Options{
			Client:          pushplugin.ClientKind(*pushClient),
			VAPIDPrivateKey: os.Getenv("WEB_PUSH_VAPID_PRIVATE_KEY"),
			VAPIDSubject:    os.Getenv("WEB_PUSH_VAPID_SUBJECT"),
			BaseURL:         os.Getenv("PUBNIGHTPICKER_WEB_BASE_URL"),
		},
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
