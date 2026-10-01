// Package pushsourcestest provides an in-memory pushsources.Source for tests.
package pushsourcestest

import (
	"context"
	"slices"
	"sync"

	"last_orders/internal/lastorders/components/pushsources"
)

type Source struct {
	mu           sync.Mutex
	CanComplete  []string
	Attendees    map[string][]string
	Participants map[string][]string
	Chats        map[string]pushsources.ChatHistory
	Acks         map[string]any
	Deleted      []string
}

func New() *Source {
	return &Source{
		Attendees:    map[string][]string{},
		Participants: map[string][]string{},
		Chats:        map[string]pushsources.ChatHistory{},
		Acks:         map[string]any{},
	}
}

func (s *Source) CanCompletePollUserIDs(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.CanComplete), nil
}

func (s *Source) AttendeeUserIDs(_ context.Context, pollID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.Attendees[pollID]), nil
}

func (s *Source) EventChatParticipantUserIDs(_ context.Context, pollID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.Participants[pollID]), nil
}

func (s *Source) ChatHistory(_ context.Context, messageID string) (pushsources.ChatHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Chats[messageID], nil
}

func (s *Source) MarkChatProcessed(_ context.Context, messageID, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.Chats[messageID]
	history.Exists, history.Processed = true, true
	s.Chats[messageID] = history
	return nil
}

func (s *Source) PushTestAck(_ context.Context, userID string) (any, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.Acks[userID]
	return value, ok, nil
}

func (s *Source) AckPushTest(_ context.Context, userID string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Acks[userID] = value
	return nil
}

func (s *Source) DeletePushTestRequest(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Deleted = append(s.Deleted, userID)
	return nil
}

// Processed reports whether a chat message has been marked processed.
func (s *Source) Processed(messageID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Chats[messageID].Processed
}

// Ack returns the acknowledged push-test value for a user.
func (s *Source) Ack(userID string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.Acks[userID]
	return value, ok
}
