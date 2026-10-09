package admindelete

import (
	"context"
	"fmt"
	"os"
	"strings"

	firebase "firebase.google.com/go/v4"
	firebaseauthsdk "firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
	"last_orders/internal/lastorders/components/firebaseauth"
)

type firebaseAuthClient struct {
	client *firebaseauthsdk.Client
}

func NewFirebaseAuthClient(ctx context.Context, projectID string, allowEmulator bool) (AuthClient, error) {
	if err := firebaseauth.CheckEmulator(allowEmulator); err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || projectID == "*detect-project-id*" {
		return nil, fmt.Errorf("a concrete Firebase Auth project ID is required")
	}
	var options []option.ClientOption
	if allowEmulator && strings.TrimSpace(os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")) != "" {
		options = append(options, option.WithoutAuthentication())
	}
	application, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, options...)
	if err != nil {
		return nil, fmt.Errorf("initialise Firebase Auth app: %w", err)
	}
	client, err := application.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialise Firebase Auth delete client: %w", err)
	}
	return &firebaseAuthClient{client: client}, nil
}

func (c *firebaseAuthClient) DeleteUser(ctx context.Context, uid string) error {
	return c.client.DeleteUser(ctx, uid)
}
