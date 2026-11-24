package meetwrite

import (
	"context"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type firestoreVoteRepository struct {
	client *firestore.Client
}

func (r *firestoreVoteRepository) ChangeVote(ctx context.Context, meetID, venueID, voterUID string, add bool) (bool, error) {
	meetRef := r.client.Collection(meetCollection).Doc(meetID)
	votesRef := r.client.Collection("votes").Doc(meetID)
	absentDelete := false
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		absentDelete = false
		meet, err := tx.Get(meetRef)
		if status.Code(err) == codes.NotFound {
			return ErrMeetNotFound
		}
		if err != nil {
			return err
		}
		groupID, ok := meet.Data()["groupId"].(string)
		if !ok || strings.TrimSpace(groupID) == "" {
			return ErrInvalidState
		}
		if add && venueID != "any" {
			candidates, _ := meet.Data()["pubs"].(map[string]any)
			if candidates == nil {
				return ErrVenueNotFound
			}
			candidate, active := candidates[venueID]
			if !active || candidate == false {
				return ErrVenueNotFound
			}
			venue, err := tx.Get(r.client.Collection("pubs").Doc(venueID))
			if status.Code(err) == codes.NotFound {
				return ErrVenueNotFound
			}
			if err != nil {
				return err
			}
			if deleted, exists := venue.Data()["deleted_on"]; exists && deleted != nil {
				return ErrVenueNotFound
			}
		}
		votes, err := tx.Get(votesRef)
		votesData := map[string]any{}
		exists := true
		if status.Code(err) == codes.NotFound {
			exists = false
		} else if err != nil {
			return err
		} else {
			votesData = votes.Data()
		}
		if !add && !exists {
			absentDelete = true
			return nil
		}
		current, valid := readStringSlice(votesData[venueID])
		if len(votesData) > 0 && !valid {
			return ErrInvalidState
		}
		found := slices.Contains(current, voterUID)
		if add {
			if found {
				return nil
			}
			updated := append(slices.Clone(current), voterUID)
			slices.Sort(updated)
			if exists {
				return tx.Update(votesRef, []firestore.Update{{Path: venueID, Value: updated}})
			}
			return tx.Create(votesRef, map[string]any{venueID: updated})
		}
		if !found {
			return nil
		}
		updated := make([]string, 0, len(current)-1)
		for _, uid := range current {
			if uid != voterUID {
				updated = append(updated, uid)
			}
		}
		if len(updated) == 0 {
			return tx.Update(votesRef, []firestore.Update{{Path: venueID, Value: firestore.Delete}})
		}
		return tx.Update(votesRef, []firestore.Update{{Path: venueID, Value: updated}})
	})
	return absentDelete, err
}

func (r *firestoreVoteRepository) Revision(ctx context.Context, meetID string) (*time.Time, error) {
	committed, err := r.client.Collection("votes").Doc(meetID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	updateTime := committed.UpdateTime
	return &updateTime, nil
}
