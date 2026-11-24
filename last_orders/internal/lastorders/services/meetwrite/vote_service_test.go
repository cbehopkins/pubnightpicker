package meetwrite

import (
	"context"
	"errors"
	"testing"
	"time"
)

type voteRepositoryStub struct {
	absentDelete bool
	changeErr    error
	revision     *time.Time
	revisionErr  error
	changeCalls  int
	readCalls    int
}

func (repository *voteRepositoryStub) ChangeVote(_ context.Context, _, _, _ string, _ bool) (bool, error) {
	repository.changeCalls++
	return repository.absentDelete, repository.changeErr
}

func (repository *voteRepositoryStub) Revision(_ context.Context, _ string) (*time.Time, error) {
	repository.readCalls++
	return repository.revision, repository.revisionErr
}

func TestChangeVoteReadsRevisionAfterSuccessfulMutation(t *testing.T) {
	laterRevision := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for _, add := range []bool{true, false} {
		repository := &voteRepositoryStub{revision: &laterRevision}
		service := &Service{votes: repository}
		result, err := service.ChangeVote(context.Background(), "meet", "venue", "voter", add)
		if err != nil {
			t.Fatal(err)
		}
		if repository.changeCalls != 1 || repository.readCalls != 1 || result.UpdateTime == nil || !result.UpdateTime.Equal(laterRevision) {
			t.Fatalf("add=%v change calls=%d revision reads=%d result=%v, want the later revision", add, repository.changeCalls, repository.readCalls, result.UpdateTime)
		}
	}
}

func TestChangeVoteAbsentDeleteDoesNotReadLaterCreation(t *testing.T) {
	laterRevision := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	repository := &voteRepositoryStub{absentDelete: true, revision: &laterRevision}
	service := &Service{votes: repository}
	result, err := service.ChangeVote(context.Background(), "meet", "venue", "voter", false)
	if err != nil {
		t.Fatal(err)
	}
	if repository.changeCalls != 1 || repository.readCalls != 0 || result.UpdateTime != nil {
		t.Fatalf("change calls=%d revision reads=%d result=%v, want no revision", repository.changeCalls, repository.readCalls, result.UpdateTime)
	}
}

func TestChangeVoteStopsOnTransactionFailure(t *testing.T) {
	want := errors.New("transaction failed")
	repository := &voteRepositoryStub{changeErr: want}
	service := &Service{votes: repository}
	_, err := service.ChangeVote(context.Background(), "meet", "venue", "voter", true)
	if !errors.Is(err, want) || repository.readCalls != 0 {
		t.Fatalf("error=%v revision reads=%d, want transaction error and no read", err, repository.readCalls)
	}
}

func TestChangeVoteReturnsRevisionReadFailure(t *testing.T) {
	want := errors.New("revision read failed")
	repository := &voteRepositoryStub{revisionErr: want}
	service := &Service{votes: repository}
	_, err := service.ChangeVote(context.Background(), "meet", "venue", "voter", true)
	if !errors.Is(err, want) || repository.changeCalls != 1 || repository.readCalls != 1 {
		t.Fatalf("error=%v change calls=%d revision reads=%d, want revision read error", err, repository.changeCalls, repository.readCalls)
	}
}
