package meets

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type firestoreGroupAuthorizer struct {
	source          groupAuthorizationSource
	now             func() time.Time
	mu              sync.Mutex
	groups          map[string]cachedGroupAuthorization
	admins          map[string]cachedAdminAuthorization
	groupFailures   map[string]cachedAuthorizationFailure
	adminFailures   map[string]cachedAuthorizationFailure
	groupGeneration map[string]uint64
	adminGeneration uint64
}

const authorizationCacheTTL = 10 * time.Second
const authorizationFailureRetryInterval = time.Second

var errAuthorizationChanged = errors.New("authorisation changed during lookup")

type groupAuthorizationDocument struct {
	Visibility string
	Members    map[string]any
}

type groupAuthorizationSource interface {
	ReadGroup(context.Context, string) (groupAuthorizationDocument, bool, error)
	ReadGlobalAdmins(context.Context) (map[string]any, bool, error)
}

type cachedGroupAuthorization struct {
	document   groupAuthorizationDocument
	exists     bool
	expiresAt  time.Time
	generation uint64
}

type cachedAdminAuthorization struct {
	allowed    bool
	expiresAt  time.Time
	generation uint64
}

type cachedAuthorizationFailure struct {
	err     error
	retryAt time.Time
}

type firestoreGroupAuthorizationSource struct {
	client *firestore.Client
}

func NewFirestoreGroupAuthorizer(client *firestore.Client) (GroupAuthorizer, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return newCachedGroupAuthorizer(firestoreGroupAuthorizationSource{client: client}, time.Now), nil
}

func (a *firestoreGroupAuthorizer) CanRead(ctx context.Context, uid, groupID string) (bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		groupGeneration, adminGeneration := a.generations(groupID)
		group, exists, invalidated, err := a.group(ctx, groupID)
		if invalidated {
			if attempt == 0 {
				continue
			}
			return false, errAuthorizationChanged
		}
		if err != nil {
			if a.changed(groupID, groupGeneration, adminGeneration) && attempt == 0 {
				continue
			}
			return false, err
		}
		allowed := exists && group.Visibility == "public"
		if exists && !allowed && uid != "" {
			allowed = isTrue(group.Members[uid])
		}
		if exists && !allowed && uid != "" {
			allowed, invalidated, err = a.globalAdmin(ctx, uid)
			if invalidated {
				if attempt == 0 {
					continue
				}
				return false, errAuthorizationChanged
			}
			if err != nil {
				if a.changed(groupID, groupGeneration, adminGeneration) && attempt == 0 {
					continue
				}
				return false, err
			}
		}
		if a.changed(groupID, groupGeneration, adminGeneration) {
			if attempt == 0 {
				continue
			}
			return false, errAuthorizationChanged
		}
		return allowed, nil
	}
	return false, errAuthorizationChanged
}

func (a *firestoreGroupAuthorizer) IsMember(ctx context.Context, uid, groupID string) (bool, error) {
	if uid == "" {
		return false, nil
	}
	group, exists, _, err := a.group(ctx, groupID)
	if err != nil {
		return false, err
	}
	return exists && isTrue(group.Members[uid]), nil
}

func newCachedGroupAuthorizer(source groupAuthorizationSource, now func() time.Time) *firestoreGroupAuthorizer {
	return &firestoreGroupAuthorizer{
		source: source, now: now,
		groups: make(map[string]cachedGroupAuthorization), admins: make(map[string]cachedAdminAuthorization),
		groupFailures: make(map[string]cachedAuthorizationFailure), adminFailures: make(map[string]cachedAuthorizationFailure),
		groupGeneration: make(map[string]uint64),
	}
}

func (a *firestoreGroupAuthorizer) group(ctx context.Context, groupID string) (groupAuthorizationDocument, bool, bool, error) {
	a.mu.Lock()
	generation := a.groupGeneration[groupID]
	if failure, ok := a.groupFailures[groupID]; ok {
		if a.now().Before(failure.retryAt) {
			a.mu.Unlock()
			return groupAuthorizationDocument{}, false, false, failure.err
		}
		delete(a.groupFailures, groupID)
	}
	if cached, ok := a.groups[groupID]; ok && cached.generation == generation && a.now().Before(cached.expiresAt) {
		a.mu.Unlock()
		return cached.document, cached.exists, false, nil
	}
	a.mu.Unlock()

	document, exists, err := a.source.ReadGroup(ctx, groupID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.groupGeneration[groupID] != generation {
		return groupAuthorizationDocument{}, false, true, nil
	}
	if err != nil {
		a.groupFailures[groupID] = cachedAuthorizationFailure{err: err, retryAt: a.now().Add(authorizationFailureRetryInterval)}
		return groupAuthorizationDocument{}, false, false, err
	}
	delete(a.groupFailures, groupID)
	a.groups[groupID] = cachedGroupAuthorization{
		document: document, exists: exists, expiresAt: a.now().Add(authorizationCacheTTL), generation: generation,
	}
	return document, exists, false, nil
}

func (a *firestoreGroupAuthorizer) globalAdmin(ctx context.Context, uid string) (bool, bool, error) {
	a.mu.Lock()
	generation := a.adminGeneration
	if failure, ok := a.adminFailures[uid]; ok {
		if a.now().Before(failure.retryAt) {
			a.mu.Unlock()
			return false, false, failure.err
		}
		delete(a.adminFailures, uid)
	}
	if cached, ok := a.admins[uid]; ok && cached.generation == generation && a.now().Before(cached.expiresAt) {
		a.mu.Unlock()
		return cached.allowed, false, nil
	}
	a.mu.Unlock()

	admins, exists, err := a.source.ReadGlobalAdmins(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.adminGeneration != generation {
		return false, true, nil
	}
	if err != nil {
		a.adminFailures[uid] = cachedAuthorizationFailure{err: err, retryAt: a.now().Add(authorizationFailureRetryInterval)}
		return false, false, err
	}
	delete(a.adminFailures, uid)
	allowed := exists && isTrue(admins[uid])
	a.admins[uid] = cachedAdminAuthorization{allowed: allowed, expiresAt: a.now().Add(authorizationCacheTTL), generation: generation}
	return allowed, false, nil
}

func (a *firestoreGroupAuthorizer) generations(groupID string) (uint64, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.groupGeneration[groupID]; !exists {
		a.groupGeneration[groupID] = 0
	}
	return a.groupGeneration[groupID], a.adminGeneration
}

func (a *firestoreGroupAuthorizer) changed(groupID string, groupGeneration, adminGeneration uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.groupGeneration[groupID] != groupGeneration || a.adminGeneration != adminGeneration
}

func (a *firestoreGroupAuthorizer) InvalidateGroup(groupID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.groupGeneration[groupID]++
	delete(a.groups, groupID)
	delete(a.groupFailures, groupID)
}

func (a *firestoreGroupAuthorizer) InvalidateAllGroups() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for groupID := range a.groupGeneration {
		a.groupGeneration[groupID]++
	}
	clear(a.groups)
	clear(a.groupFailures)
}

func (a *firestoreGroupAuthorizer) InvalidateGlobalAdmins() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.adminGeneration++
	clear(a.admins)
	clear(a.adminFailures)
}

func (s firestoreGroupAuthorizationSource) ReadGroup(ctx context.Context, groupID string) (groupAuthorizationDocument, bool, error) {
	document, err := s.client.Collection("groups").Doc(groupID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return groupAuthorizationDocument{}, false, nil
	}
	if err != nil {
		return groupAuthorizationDocument{}, false, err
	}
	data := document.Data()
	visibility, _ := data["visibility"].(string)
	members, _ := data["members"].(map[string]any)
	return groupAuthorizationDocument{Visibility: visibility, Members: members}, true, nil
}

func (s firestoreGroupAuthorizationSource) ReadGlobalAdmins(ctx context.Context) (map[string]any, bool, error) {
	document, err := s.client.Collection("roles").Doc("admin").Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return document.Data(), true, nil
}

func isTrue(value any) bool {
	allowed, ok := value.(bool)
	return ok && allowed
}
