package diagnosticsconfig

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	for _, test := range []struct {
		data    map[string]any
		want    bool
		invalid bool
	}{
		{nil, false, false}, {map[string]any{}, false, false},
		{map[string]any{SilenceField: true}, true, false},
		{map[string]any{SilenceField: false}, false, false},
		{map[string]any{SilenceField: "true"}, false, true},
		{map[string]any{SilenceField: nil}, false, true},
	} {
		got, err := Decode(test.data)
		if got != test.want || (err != nil) != test.invalid {
			t.Fatalf("Decode(%v) = %t, %v", test.data, got, err)
		}
	}
}

func TestReadinessRetentionAndCancellation(t *testing.T) {
	updates := make(chan bool)
	failed := make(chan struct{})
	service := New(func(ctx context.Context, update func(bool)) error {
		select {
		case value := <-updates:
			update(value)
			close(failed)
			return errors.New("disconnected")
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil)
	if _, known := service.Snapshot(); known {
		t.Fatal("known before initial snapshot")
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	updates <- true
	select {
	case <-service.Ready():
	case <-time.After(time.Second):
		t.Fatal("not ready")
	}
	<-failed
	if value, known := service.Snapshot(); !known || !value {
		t.Fatal("lost last known value")
	}
}

func TestRuntimeUpdates(t *testing.T) {
	updates := make(chan bool)
	applied := make(chan struct{})
	service := New(func(ctx context.Context, update func(bool)) error {
		for {
			select {
			case value := <-updates:
				update(value)
				applied <- struct{}{}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}, nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, value := range []bool{false, true, false} {
		updates <- value
		<-applied
		if got, known := service.Snapshot(); got != value || !known {
			t.Fatalf("snapshot = %t, %t", got, known)
		}
	}
}

func TestInitialFailureDoesNotEstablishReadiness(t *testing.T) {
	called := make(chan struct{})
	service := New(func(ctx context.Context, update func(bool)) error { close(called); return errors.New("unavailable") }, nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-called
	if _, known := service.Snapshot(); known {
		t.Fatal("initial failure defaulted to live")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}
