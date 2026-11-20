package meets

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/currentmeetcache"
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
