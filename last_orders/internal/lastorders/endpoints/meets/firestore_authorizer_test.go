package meets

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/currentmeetcache"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

type groupAuthorizationSourceStub struct {
	mu            sync.Mutex
	group         groupAuthorizationDocument
	groupExists   bool
	groupCalls    int
	groupStarted  chan struct{}
	groupContinue chan struct{}
	admins        map[string]any
	adminsExists  bool
	adminCalls    int
	adminStarted  chan struct{}
	adminContinue chan struct{}
	groupReadErr  error
	adminReadErr  error
}

func (s *groupAuthorizationSourceStub) ReadGroup(_ context.Context, _ string) (groupAuthorizationDocument, bool, error) {
	s.mu.Lock()
	s.groupCalls++
	document, exists, err := s.group, s.groupExists, s.groupReadErr
	started, proceed := s.groupStarted, s.groupContinue
	if started != nil {
		s.groupStarted = nil
	}
	s.mu.Unlock()
	if started != nil {
		close(started)
		<-proceed
	}
	return document, exists, err
}

func (s *groupAuthorizationSourceStub) ReadGlobalAdmins(context.Context) (map[string]any, bool, error) {
	s.mu.Lock()
	s.adminCalls++
	admins, exists, err := s.admins, s.adminsExists, s.adminReadErr
	started, proceed := s.adminStarted, s.adminContinue
	if started != nil {
		s.adminStarted = nil
	}
	s.mu.Unlock()
	if started != nil {
		close(started)
		<-proceed
	}
	return admins, exists, err
}

func (s *groupAuthorizationSourceStub) callCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groupCalls, s.adminCalls
}

func TestGroupAuthorizerCachesUntilTTLAndRetriesInvalidatedLookup(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &groupAuthorizationSourceStub{
		group:       groupAuthorizationDocument{Visibility: "private", Members: map[string]any{"caller": true}},
		groupExists: true,
	}
	authorizer := newCachedGroupAuthorizer(source, func() time.Time { return now })

	allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a")
	if err != nil || !allowed {
		t.Fatalf("initial member decision = (%v, %v), want (true, nil)", allowed, err)
	}
	allowed, err = authorizer.CanRead(context.Background(), "caller", "group-a")
	groupCalls, _ := source.callCounts()
	if err != nil || !allowed || groupCalls != 1 {
		t.Fatalf("cached member decision = (%v, %v), group reads = %d; want true, nil, 1", allowed, err, groupCalls)
	}

	now = now.Add(authorizationCacheTTL)
	source.group = groupAuthorizationDocument{Visibility: "private", Members: map[string]any{}}
	allowed, err = authorizer.CanRead(context.Background(), "caller", "group-a")
	groupCalls, _ = source.callCounts()
	if err != nil || allowed || groupCalls != 2 {
		t.Fatalf("expired member decision = (%v, %v), group reads = %d; want false, nil, 2", allowed, err, groupCalls)
	}

	source.group = groupAuthorizationDocument{Visibility: "private", Members: map[string]any{"caller": true}}
	groupStarted := make(chan struct{})
	groupContinue := make(chan struct{})
	source.groupStarted = groupStarted
	source.groupContinue = groupContinue
	decision := make(chan struct {
		allowed bool
		err     error
	}, 1)
	go func() {
		allowed, err := authorizer.CanRead(context.Background(), "caller", "group-b")
		decision <- struct {
			allowed bool
			err     error
		}{allowed: allowed, err: err}
	}()
	<-groupStarted
	authorizer.InvalidateGroup("group-b")
	source.mu.Lock()
	source.group = groupAuthorizationDocument{Visibility: "private", Members: map[string]any{}}
	source.mu.Unlock()
	close(groupContinue)
	result := <-decision
	groupCalls, _ = source.callCounts()
	if result.err != nil || result.allowed || groupCalls != 4 {
		t.Fatalf("raced member decision = (%v, %v), group reads = %d; want false, nil after one retry", result.allowed, result.err, groupCalls)
	}
}

func TestGroupAuthorizerDoesNotCacheRefreshErrors(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &groupAuthorizationSourceStub{groupReadErr: errors.New("Firestore unavailable")}
	authorizer := newCachedGroupAuthorizer(source, func() time.Time { return now })
	if _, err := authorizer.CanRead(context.Background(), "caller", "group-a"); err == nil {
		t.Fatal("first read error = nil, want source failure")
	}
	source.mu.Lock()
	source.groupReadErr = nil
	source.group = groupAuthorizationDocument{Visibility: "public"}
	source.groupExists = true
	source.mu.Unlock()
	if _, err := authorizer.CanRead(context.Background(), "caller", "group-a"); err == nil {
		t.Fatal("immediate retry error = nil, want the uncached source failure during retry interval")
	}
	groupCalls, _ := source.callCounts()
	if groupCalls != 1 {
		t.Fatalf("group reads before retry interval=%d, want 1", groupCalls)
	}
	now = now.Add(authorizationFailureRetryInterval)
	allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a")
	groupCalls, _ = source.callCounts()
	if err != nil || !allowed || groupCalls != 2 {
		t.Fatalf("recovered decision = (%v, %v), group reads = %d; want true, nil, 2", allowed, err, groupCalls)
	}
}

func TestMeetAPIRecoversAfterAuthorizationSourceFailure(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &groupAuthorizationSourceStub{groupReadErr: errors.New("Firestore unavailable")}
	authorizer := newCachedGroupAuthorizer(source, func() time.Time { return now })
	endpoint := &Endpoint{
		Current: &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
			Date: "2026-11-20", Generation: "generation-1",
		}},
		Groups: authorizer, Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, authenticatedPastRequest("/api/meets/group-a/current"))
		return response
	}
	if response := request(); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("source failure status=%d body=%s, want HTTP 503", response.Code, response.Body)
	}
	source.mu.Lock()
	source.groupReadErr = nil
	source.group = groupAuthorizationDocument{Visibility: "public"}
	source.groupExists = true
	source.mu.Unlock()
	if response := request(); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("immediate retry status=%d body=%s, want HTTP 503 during retry interval", response.Code, response.Body)
	}
	now = now.Add(authorizationFailureRetryInterval)
	if response := request(); response.Code != http.StatusOK {
		t.Fatalf("recovered Meet GET status=%d body=%s, want HTTP 200", response.Code, response.Body)
	}
}

type controllableFirestoreAuthorizationSource struct {
	base        groupAuthorizationSource
	mu          sync.Mutex
	readErr     error
	readStarted chan struct{}
	releaseRead chan struct{}
}

func (s *controllableFirestoreAuthorizationSource) ReadGroup(ctx context.Context, groupID string) (groupAuthorizationDocument, bool, error) {
	s.mu.Lock()
	readErr, started, release := s.readErr, s.readStarted, s.releaseRead
	s.readStarted, s.releaseRead = nil, nil
	s.mu.Unlock()
	if readErr != nil {
		return groupAuthorizationDocument{}, false, readErr
	}
	document, exists, err := s.base.ReadGroup(ctx, groupID)
	if err != nil {
		return groupAuthorizationDocument{}, false, err
	}
	if started != nil {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return groupAuthorizationDocument{}, false, ctx.Err()
		}
	}
	return document, exists, nil
}

func (s *controllableFirestoreAuthorizationSource) ReadGlobalAdmins(ctx context.Context) (map[string]any, bool, error) {
	return s.base.ReadGlobalAdmins(ctx)
}

func (s *controllableFirestoreAuthorizationSource) setReadError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readErr = err
}

func (s *controllableFirestoreAuthorizationSource) blockNextRead() (<-chan struct{}, func()) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.mu.Lock()
	s.readStarted, s.releaseRead = started, release
	s.mu.Unlock()
	return started, func() { once.Do(func() { close(release) }) }
}

func TestMeetAPIUsesFirestoreAuthorityCacheAgainstEmulator(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-meet-auth-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	groups := []string{"ttl-group", "admin-ttl-group", "race-group", "recovery-group"}
	for _, groupID := range groups {
		data := map[string]any{
			"name": groupID, "visibility": "public",
		}
		if groupID == "admin-ttl-group" {
			data["visibility"] = "private"
			data["members"] = map[string]any{}
		}
		if _, err := client.Collection("groups").Doc(groupID).Set(ctx, data); err != nil {
			t.Fatal(err)
		}
	}
	adminRole := client.Collection("roles").Doc("admin")
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		for _, groupID := range groups {
			_, _ = client.Collection("groups").Doc(groupID).Delete(cleanupCtx)
		}
		_, _ = adminRole.Delete(cleanupCtx)
	})

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &controllableFirestoreAuthorizationSource{base: firestoreGroupAuthorizationSource{client: client}}
	authorizer := newCachedGroupAuthorizer(source, func() time.Time { return now })
	endpoint := &Endpoint{
		Current: &currentReaderStub{date: "2026-11-20", snapshot: currentmeetcache.Snapshot{
			Date: "2026-11-20", Generation: "generation-1",
		}},
		Groups: authorizer, Cursors: testCurrentCursorCodec(t), Auth: pastMeetAuthStub{},
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	clientHTTP := server.Client()
	get := func(groupID string) int {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/meets/"+groupID+"/current", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer valid-token")
		response, err := clientHTTP.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}

	if status := get("ttl-group"); status != http.StatusOK {
		t.Fatalf("initial public Group GET status=%d, want HTTP 200", status)
	}
	if _, err := client.Collection("groups").Doc("ttl-group").Update(ctx, []firestore.Update{
		{Path: "visibility", Value: "private"},
		{Path: "members", Value: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	if status := get("ttl-group"); status != http.StatusOK {
		t.Fatalf("cached public decision status=%d, want HTTP 200 before TTL expiry", status)
	}
	now = now.Add(authorizationCacheTTL)
	if status := get("ttl-group"); status != http.StatusNotFound {
		t.Fatalf("expired public decision status=%d, want private-Group HTTP 404", status)
	}
	if _, err := adminRole.Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatal(err)
	}
	authorizer.InvalidateGlobalAdmins()
	if status := get("admin-ttl-group"); status != http.StatusOK {
		t.Fatalf("global-admin grant status=%d, want HTTP 200", status)
	}
	if _, err := adminRole.Set(ctx, map[string]any{"verified-user": false}); err != nil {
		t.Fatal(err)
	}
	if status := get("admin-ttl-group"); status != http.StatusOK {
		t.Fatalf("cached global-admin decision status=%d, want HTTP 200 before TTL expiry", status)
	}
	now = now.Add(authorizationCacheTTL)
	if status := get("admin-ttl-group"); status != http.StatusNotFound {
		t.Fatalf("expired global-admin decision status=%d, want HTTP 404", status)
	}

	authorizer.InvalidateGroup("recovery-group")
	source.setReadError(errors.New("simulated Firestore outage"))
	if status := get("recovery-group"); status != http.StatusServiceUnavailable {
		t.Fatalf("authority outage status=%d, want HTTP 503", status)
	}
	source.setReadError(nil)
	if status := get("recovery-group"); status != http.StatusServiceUnavailable {
		t.Fatalf("immediate recovery retry status=%d, want HTTP 503 during backoff", status)
	}
	now = now.Add(authorizationFailureRetryInterval)
	if status := get("recovery-group"); status != http.StatusOK {
		t.Fatalf("recovered authority GET status=%d, want HTTP 200", status)
	}

	readStarted, releaseRead := source.blockNextRead()
	defer releaseRead()
	decision := make(chan int, 1)
	go func() { decision <- get("race-group") }()
	select {
	case <-readStarted:
	case <-ctx.Done():
		t.Fatal("Firestore-backed authorization lookup did not start")
	}
	if _, err := client.Collection("groups").Doc("race-group").Update(ctx, []firestore.Update{
		{Path: "visibility", Value: "private"},
		{Path: "members", Value: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	authorizer.InvalidateGroup("race-group")
	releaseRead()
	select {
	case status := <-decision:
		if status != http.StatusNotFound {
			t.Fatalf("invalidation-raced API status=%d, want HTTP 404 after fresh retry", status)
		}
	case <-ctx.Done():
		t.Fatal("Meet API did not finish its invalidation-raced authorization retry")
	}
}

func TestGroupAuthorizerGrantsOnlyTrueMembershipAndAdminValues(t *testing.T) {
	for _, test := range []struct {
		name         string
		group        groupAuthorizationDocument
		groupExists  bool
		admins       map[string]any
		adminsExists bool
		want         bool
	}{
		{name: "missing group", want: false},
		{name: "public visibility", group: groupAuthorizationDocument{Visibility: "public"}, groupExists: true, want: true},
		{name: "true member", group: groupAuthorizationDocument{Visibility: "private", Members: map[string]any{"caller": true}}, groupExists: true, want: true},
		{name: "false member", group: groupAuthorizationDocument{Visibility: "private", Members: map[string]any{"caller": false}}, groupExists: true, adminsExists: true, want: false},
		{name: "malformed member value", group: groupAuthorizationDocument{Visibility: "private", Members: map[string]any{"caller": "true"}}, groupExists: true, adminsExists: true, want: false},
		{name: "true global administrator", group: groupAuthorizationDocument{Visibility: "private"}, groupExists: true, admins: map[string]any{"caller": true}, adminsExists: true, want: true},
		{name: "malformed global administrator value", group: groupAuthorizationDocument{Visibility: "private"}, groupExists: true, admins: map[string]any{"caller": "true"}, adminsExists: true, want: false},
		{name: "missing administrator document", group: groupAuthorizationDocument{Visibility: "private"}, groupExists: true, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := &groupAuthorizationSourceStub{
				group: test.group, groupExists: test.groupExists,
				admins: test.admins, adminsExists: test.adminsExists,
			}
			authorizer := newCachedGroupAuthorizer(source, time.Now)
			allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a")
			if err != nil || allowed != test.want {
				t.Fatalf("CanRead() = (%v, %v), want (%v, nil)", allowed, err, test.want)
			}
		})
	}
}

func TestGroupAuthorizerRetriesInvalidatedGlobalAdminLookup(t *testing.T) {
	source := &groupAuthorizationSourceStub{
		group: groupAuthorizationDocument{Visibility: "private"}, groupExists: true,
		admins: map[string]any{"caller": true}, adminsExists: true,
	}
	adminStarted := make(chan struct{})
	adminContinue := make(chan struct{})
	source.adminStarted = adminStarted
	source.adminContinue = adminContinue
	authorizer := newCachedGroupAuthorizer(source, time.Now)
	decision := make(chan struct {
		allowed bool
		err     error
	}, 1)
	go func() {
		allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a")
		decision <- struct {
			allowed bool
			err     error
		}{allowed: allowed, err: err}
	}()
	<-adminStarted
	authorizer.InvalidateGlobalAdmins()
	source.mu.Lock()
	source.admins = map[string]any{"caller": false}
	source.mu.Unlock()
	close(adminContinue)
	result := <-decision
	_, adminCalls := source.callCounts()
	if result.err != nil || result.allowed || adminCalls != 2 {
		t.Fatalf("raced global-admin decision = (%v, %v), role reads = %d; want false, nil after one retry", result.allowed, result.err, adminCalls)
	}
}

func TestGroupAuthorizerExpiresGlobalAdminDecision(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	source := &groupAuthorizationSourceStub{
		group: groupAuthorizationDocument{Visibility: "private"}, groupExists: true,
		admins: map[string]any{"caller": true}, adminsExists: true,
	}
	authorizer := newCachedGroupAuthorizer(source, func() time.Time { return now })
	allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a")
	if err != nil || !allowed {
		t.Fatalf("initial global-admin decision = (%v, %v), want (true, nil)", allowed, err)
	}
	source.admins = map[string]any{"caller": false}
	allowed, err = authorizer.CanRead(context.Background(), "caller", "group-a")
	_, adminCalls := source.callCounts()
	if err != nil || !allowed || adminCalls != 1 {
		t.Fatalf("cached global-admin decision = (%v, %v), role reads = %d; want true, nil, 1", allowed, err, adminCalls)
	}
	now = now.Add(authorizationCacheTTL)
	allowed, err = authorizer.CanRead(context.Background(), "caller", "group-a")
	_, adminCalls = source.callCounts()
	if err != nil || allowed || adminCalls != 2 {
		t.Fatalf("expired global-admin decision = (%v, %v), role reads = %d; want false, nil, 2", allowed, err, adminCalls)
	}
}

func TestGroupAuthorizerInvalidatesAllGroupsOnNewBaseline(t *testing.T) {
	source := &groupAuthorizationSourceStub{
		group: groupAuthorizationDocument{Visibility: "public"}, groupExists: true,
	}
	authorizer := newCachedGroupAuthorizer(source, time.Now)
	if allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a"); err != nil || !allowed {
		t.Fatalf("initial public decision = (%v, %v), want (true, nil)", allowed, err)
	}
	source.group = groupAuthorizationDocument{Visibility: "private"}
	authorizer.InvalidateAllGroups()
	if allowed, err := authorizer.CanRead(context.Background(), "caller", "group-a"); err != nil || allowed {
		t.Fatalf("post-baseline private decision = (%v, %v), want (false, nil)", allowed, err)
	}
}
