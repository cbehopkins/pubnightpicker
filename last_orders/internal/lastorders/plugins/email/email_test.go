package email_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
	durableemail "durable_email"
	emailplugin "last_orders/internal/lastorders/plugins/email"

	_ "modernc.org/sqlite"
)

func TestNewRejectsMissingAndUnknownClientKinds(t *testing.T) {
	for _, kind := range []emailplugin.ClientKind{"", "carrier-pigeon"} {
		if _, err := emailplugin.New(openDB(t), emailplugin.Options{Client: kind}); err == nil {
			t.Errorf("New(%q) error = nil, want an error", kind)
		}
	}
}

func TestDummyPluginRegistersEveryDurableEmailHandler(t *testing.T) {
	db := openDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatalf("init cellar store: %v", err)
	}
	plugin, err := emailplugin.New(db, emailplugin.Options{Client: emailplugin.ClientDummy})
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
