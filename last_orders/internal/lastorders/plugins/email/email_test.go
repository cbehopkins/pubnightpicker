package email_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
	durableemail "durable_email"
	"last_orders/internal/lastorders/components/ratelimit"
	emailplugin "last_orders/internal/lastorders/plugins/email"

	_ "modernc.org/sqlite"
)

func TestNewRejectsMissingAndUnknownClientKinds(t *testing.T) {
	for _, kind := range []emailplugin.ClientKind{"", "carrier-pigeon"} {
		if _, err := emailplugin.New(openDB(t), emailplugin.Options{Client: kind, Tokens: emailTokens(t)}); err == nil {
			t.Errorf("New(%q) error = nil, want an error", kind)
		}
	}
}

func TestNewSweegoRequiresCredentials(t *testing.T) {
	if _, err := emailplugin.New(openDB(t), emailplugin.Options{Client: emailplugin.ClientSweego, Tokens: emailTokens(t)}); err == nil {
		t.Fatal("Sweego without credentials returned nil error")
	}
}

func TestNewSweegoAcceptsCredentials(t *testing.T) {
	plugin, err := emailplugin.New(openDB(t), emailplugin.Options{
		Client:         emailplugin.ClientSweego,
		Tokens:         emailTokens(t),
		SweegoToken:    "token",
		SweegoProvider: "email.example.test",
		SweegoBaseURL:  "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("new Sweego plugin: %v", err)
	}
	if plugin == nil {
		t.Fatal("new Sweego plugin returned nil plugin")
	}
}

func TestNewMailtrapRequiresToken(t *testing.T) {
	if _, err := emailplugin.New(openDB(t), emailplugin.Options{Client: emailplugin.ClientMailtrap, Tokens: emailTokens(t)}); err == nil {
		t.Fatal("Mailtrap without a token returned nil error")
	}
}

func TestNewMailtrapAcceptsToken(t *testing.T) {
	plugin, err := emailplugin.New(openDB(t), emailplugin.Options{
		Client:        emailplugin.ClientMailtrap,
		Tokens:        emailTokens(t),
		MailtrapToken: "token",
	})
	if err != nil {
		t.Fatalf("new Mailtrap plugin: %v", err)
	}
	if plugin == nil {
		t.Fatal("new Mailtrap plugin returned nil plugin")
	}
}

func TestNewRejectsBothProviderTokens(t *testing.T) {
	_, err := emailplugin.New(openDB(t), emailplugin.Options{
		Client:        emailplugin.ClientMailtrap,
		MailtrapToken: "mailtrap-token",
		Tokens:        emailTokens(t),
		SweegoToken:   "sweego-token",
	})
	if err == nil {
		t.Fatal("both provider tokens returned nil error")
	}
}

func TestDummyPluginRegistersEveryDurableEmailHandler(t *testing.T) {
	db := openDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatalf("init cellar store: %v", err)
	}
	plugin, err := emailplugin.New(db, emailplugin.Options{Client: emailplugin.ClientDummy, Tokens: emailTokens(t)})
	if err != nil {
		t.Fatalf("new plugin: %v", err)
	}
	runtime := cellar.New(cellarStore, cellar.Config{})
	if err := plugin.Register(runtime); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	for _, name := range []cellar.HandlerName{
		durableemail.HandlerSetup,
		durableemail.HandlerRecovery,
		durableemail.HandlerRecoveryFanout,
		durableemail.HandlerPost,
		durableemail.HandlerVerify,
	} {
		if err := runtime.Register(name, noopHandler{}); !errors.Is(err, cellar.ErrHandlerAlreadyRegistered) {
			t.Errorf("re-registering %q: error = %v, want %v", name, err, cellar.ErrHandlerAlreadyRegistered)
		}
	}
}

type guardedTokens struct {
	wait   int
	err    error
	counts []int
}

func (source *guardedTokens) Acquire() int { panic("email delivery must use AcquireN") }
func (source *guardedTokens) AcquireN(count int) (int, error) {
	source.counts = append(source.counts, count)
	return source.wait, source.err
}

func TestEmailGuardDefersPendingRecipientsWithoutAcceptance(t *testing.T) {
	for _, test := range []struct {
		name    string
		source  *guardedTokens
		delay   time.Duration
		wantLog string
	}{
		{name: "daily shortage", source: &guardedTokens{wait: 3600}, delay: time.Hour},
		{name: "oversized", source: &guardedTokens{err: &ratelimit.RequestExceedsCapacityError{Source: "email.send", Count: 2, Maximum: 1}}, delay: 24 * time.Hour, wantLog: "email batch exceeds daily capacity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openDB(t)
			store, err := cellarsqlite.NewStore(db, nil)
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			plugin, err := emailplugin.New(db, emailplugin.Options{Client: emailplugin.ClientDummy, Tokens: test.source, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
			if err != nil {
				t.Fatal(err)
			}
			runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
			if err := plugin.Register(runtime); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Register("test.marker", noopHandler{}); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{"poll-opened:guarded", "poll-completed:guarded:email:key"} {
				request := durableemail.SendRequest{IdempotencyToken: token, SenderEmail: "sender@example.com", Subject: "Hi", Text: "Hi", Recipients: []durableemail.SendRecipient{{Email: "alice@example.com"}, {Email: "bob@example.com"}}}
				steps := append(durableemail.NewSendSequence(request), cellar.Step{HandlerName: "test.marker", Payload: struct{}{}})
				if _, err := runtime.AddSequence(steps...); err != nil {
					t.Fatal(err)
				}
			}
			before := time.Now().UTC()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if err := runtime.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if len(test.source.counts) != 2 || test.source.counts[0] != 2 || test.source.counts[1] != 2 {
				t.Fatalf("acquisitions = %v", test.source.counts)
			}
			active, err := store.ListActive()
			if err != nil || len(active) != 2 {
				t.Fatalf("active = %+v, %v", active, err)
			}
			for _, cell := range active {
				if cell.CurrentStep != 2 || cell.NotBefore == nil || cell.NotBefore.Before(before.Add(test.delay)) || cell.NotBefore.After(time.Now().Add(test.delay)) {
					t.Fatalf("deferred cell = %+v", cell)
				}
			}
			var untouched int
			if err := db.QueryRow(`SELECT COUNT(*) FROM email_progress WHERE state = 'Pending' AND submitted_at IS NULL AND pmuid IS NULL`).Scan(&untouched); err != nil || untouched != 4 {
				t.Fatalf("untouched = %d, %v", untouched, err)
			}
			if strings.Contains(logs.String(), "dummy email sent") || test.wantLog != "" && !strings.Contains(logs.String(), test.wantLog) {
				t.Fatalf("logs = %s", logs.String())
			}
		})
	}
}

func TestEmailGuardKeepsDiagnosticsSeparateAndFailsClosed(t *testing.T) {
	for _, token := range []string{"test-email:alice:request", "unknown", "poll-opened:failure"} {
		t.Run(token, func(t *testing.T) {
			db := openDB(t)
			store, err := cellarsqlite.NewStore(db, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := &guardedTokens{err: errors.New("source failed")}
			plugin, err := emailplugin.New(db, emailplugin.Options{Client: emailplugin.ClientDummy, Tokens: source})
			if err != nil {
				t.Fatal(err)
			}
			runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
			if err := plugin.Register(runtime); err != nil {
				t.Fatal(err)
			}
			request := durableemail.SendRequest{IdempotencyToken: token, SenderEmail: "sender@example.com", Subject: "Hi", Text: "Hi", Recipients: []durableemail.SendRecipient{{Email: "alice@example.com"}}}
			if _, err := runtime.AddSequence(durableemail.NewSendSequence(request)...); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err = runtime.Start(ctx)
			var state string
			var submitted sql.NullTime
			if scanErr := db.QueryRow(`SELECT state, submitted_at FROM email_progress WHERE idempotency_token = ?`, token).Scan(&state, &submitted); scanErr != nil {
				t.Fatal(scanErr)
			}
			if strings.HasPrefix(token, "test-email:") {
				if err != nil || state != "Accepted" || len(source.counts) != 0 {
					t.Fatalf("diagnostics: %s, %v, %v", state, err, source.counts)
				}
			} else if err == nil || state != "Pending" || submitted.Valid {
				t.Fatalf("unprotected send: state %s, submitted %v, err %v", state, submitted, err)
			}
		})
	}
}

func TestNewRequiresEmailTokenSource(t *testing.T) {
	if _, err := emailplugin.New(openDB(t), emailplugin.Options{Client: emailplugin.ClientDummy}); err == nil {
		t.Fatal("missing source accepted")
	}
}

func emailTokens(t *testing.T) ratelimit.TokenSource {
	t.Helper()
	tokens, err := ratelimit.New("email.send", 100, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

type noopHandler struct{}

func (noopHandler) Handle(context.Context, struct{}) cellar.Result { return cellar.Complete{} }

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "email.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
