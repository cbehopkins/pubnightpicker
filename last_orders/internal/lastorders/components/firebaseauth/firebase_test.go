package firebaseauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func TestEmulatorGuard(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	if _, err := New(context.Background(), "test-project", false); err == nil {
		t.Fatal("emulator enabled without opt-in")
	}
	for _, host := range []string{"https://localhost:9099", "remote.example:9099", "0.0.0.0:9099"} {
		t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", host)
		if err := CheckEmulator(true); err == nil {
			t.Fatalf("accepted non-loopback emulator %q", host)
		}
	}
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	if _, err := New(context.Background(), "", true); err == nil {
		t.Fatal("emulator must have an explicit project")
	}
	if _, err := New(context.Background(), "*detect-project-id*", true); err == nil {
		t.Fatal("accepted Firestore sentinel")
	}
}

func TestFirebaseVerifierRejectsMalformedToken(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	verifier, err := New(context.Background(), "test-project", true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = verifier.Verify(context.Background(), "not-a-jwt")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected authentication refusal, got %v", err)
	}
}

func TestFirebaseVerifierTokenClaims(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	verifier, err := New(context.Background(), "test-project", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, project, issuer, subject string
		expires                        int64
	}{
		{name: "wrong project", project: "other-project", issuer: "https://securetoken.google.com/other-project", subject: "emulator-user", expires: time.Now().Add(time.Hour).Unix()},
		{name: "wrong issuer", project: "test-project", issuer: "https://evil.example/test-project", subject: "emulator-user", expires: time.Now().Add(time.Hour).Unix()},
		{name: "expired", project: "test-project", issuer: "https://securetoken.google.com/test-project", subject: "emulator-user", expires: time.Now().Add(-time.Hour).Unix()},
		{name: "empty subject", project: "test-project", issuer: "https://securetoken.google.com/test-project", expires: time.Now().Add(time.Hour).Unix()},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{"aud": test.project, "iss": test.issuer, "sub": test.subject, "exp": test.expires, "iat": time.Now().Add(-time.Minute).Unix(), "auth_time": time.Now().Add(-time.Minute).Unix()}
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
			_, err = verifier.Verify(context.Background(), token)
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("expected authentication refusal, got %v", err)
			}
		})
	}
}
