package firebaseauth

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

type FirebaseVerifier struct {
	client *auth.Client
}

func CheckEmulator(allow bool) error {
	host := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	if host == "" {
		return nil
	}
	if !allow {
		return fmt.Errorf("FIREBASE_AUTH_EMULATOR_HOST requires explicit -allow-auth-emulator development opt-in")
	}
	hostname, _, err := net.SplitHostPort(host)
	address := net.ParseIP(hostname)
	if err != nil || (hostname != "localhost" && (address == nil || !address.IsLoopback())) {
		return fmt.Errorf("Firebase Auth emulator must use a loopback host:port")
	}
	return nil
}

func New(ctx context.Context, projectID string, allowEmulator bool) (*FirebaseVerifier, error) {
	if err := CheckEmulator(allowEmulator); err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "*detect-project-id*" {
		return nil, fmt.Errorf("Firebase Auth requires a concrete project ID, not the Firestore detection sentinel")
	}
	var options []option.ClientOption
	if os.Getenv("FIREBASE_AUTH_EMULATOR_HOST") != "" {
		if projectID == "" {
			return nil, fmt.Errorf("FIREBASE_AUTH_PROJECT_ID is required with the Auth emulator")
		}
		options = append(options, option.WithoutAuthentication())
	}
	application, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, options...)
	if err != nil {
		return nil, fmt.Errorf("initialise Firebase Auth app: %w", err)
	}
	client, err := application.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialise Firebase Auth client (configure FIREBASE_AUTH_PROJECT_ID or application default credentials): %w", err)
	}
	return &FirebaseVerifier{client: client}, nil
}

func (verifier *FirebaseVerifier) Verify(ctx context.Context, token string) (Principal, error) {
	verified, err := verifier.client.VerifyIDToken(ctx, token)
	if err != nil {
		if auth.IsIDTokenInvalid(err) || auth.IsIDTokenExpired(err) || auth.IsIDTokenRevoked(err) || auth.IsUserDisabled(err) || auth.IsUserNotFound(err) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, err
	}
	if verified.UID == "" {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{UID: verified.UID}, nil
}
