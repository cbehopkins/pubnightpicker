package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"

	"cloud.google.com/go/firestore"
	"golang.org/x/oauth2/google"
)

type firebaseConfiguration struct {
	credentials        *google.Credentials
	firestoreProjectID string
	authProjectID      string
}

func firebaseConfigFromEnv(ctx context.Context, lookup func(string) (string, bool)) (firebaseConfiguration, error) {
	getenv := func(name string) string {
		value, _ := lookup(name)
		return strings.TrimSpace(value)
	}
	config := firebaseConfiguration{firestoreProjectID: getenv("GOOGLE_CLOUD_PROJECT")}
	raw, configured := lookup("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON")
	if configured {
		if getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
			return firebaseConfiguration{}, fmt.Errorf("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON and GOOGLE_APPLICATION_CREDENTIALS are mutually exclusive")
		}
		var account struct {
			Type        string `json:"type"`
			ProjectID   string `json:"project_id"`
			ClientEmail string `json:"client_email"`
			PrivateKey  string `json:"private_key"`
		}
		if err := json.Unmarshal([]byte(raw), &account); err != nil || account.Type != "service_account" ||
			strings.TrimSpace(account.ProjectID) == "" || strings.TrimSpace(account.ClientEmail) == "" || account.PrivateKey == "" {
			return firebaseConfiguration{}, fmt.Errorf("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON must contain service-account JSON with project_id, client_email and private_key")
		}
		block, _ := pem.Decode([]byte(account.PrivateKey))
		if block == nil {
			return firebaseConfiguration{}, fmt.Errorf("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON must contain a valid RSA private_key")
		}
		privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		}
		if _, valid := privateKey.(*rsa.PrivateKey); err != nil || !valid {
			return firebaseConfiguration{}, fmt.Errorf("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON must contain a valid RSA private_key")
		}
		config.credentials, err = google.CredentialsFromJSONWithType(ctx, []byte(raw), google.ServiceAccount,
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/firebase",
			"https://www.googleapis.com/auth/identitytoolkit",
			"https://www.googleapis.com/auth/userinfo.email")
		if err != nil {
			return firebaseConfiguration{}, fmt.Errorf("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON contains invalid service-account credentials")
		}
		if config.firestoreProjectID == "" && getenv("FIRESTORE_EMULATOR_HOST") == "" {
			config.firestoreProjectID = account.ProjectID
		}
	}
	if config.firestoreProjectID == "" {
		if getenv("FIRESTORE_EMULATOR_HOST") != "" {
			config.firestoreProjectID = "last-orders-emulator"
		} else {
			config.firestoreProjectID = firestore.DetectProjectID
		}
	}
	config.authProjectID = getenv("FIREBASE_AUTH_PROJECT_ID")
	if config.authProjectID == "" && config.firestoreProjectID != firestore.DetectProjectID {
		config.authProjectID = config.firestoreProjectID
	}
	return config, nil
}
