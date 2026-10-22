package app

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDialFirestoreIPv4(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("IPv4", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		connection, err := dialFirestoreIPv4(ctx, listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		peer, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer peer.Close()
		if address := connection.RemoteAddr().(*net.TCPAddr); address.IP.To4() == nil {
			t.Fatalf("expected IPv4 peer, got %s", address)
		}
	})
	t.Run("IPv6 rejected", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		connection, err := dialFirestoreIPv4(ctx, net.JoinHostPort("::1", port))
		if err == nil {
			connection.Close()
			t.Fatal("IPv4 dialer must reject an IPv6 address")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		connection, err := dialFirestoreIPv4(ctx, listener.Addr().String())
		if err == nil {
			connection.Close()
			t.Fatal("dialer must honour cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	})
}
