package meetwrite

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrInvalidInput  = errors.New("invalid Meet input")
	ErrGroupNotFound = errors.New("Group not found")
	ErrMeetNotFound  = errors.New("Meet not found")
	ErrVenueNotFound = errors.New("Venue not found")
	ErrInvalidState  = errors.New("invalid Meet state")
)

const (
	meetCollection  = "polls"
	groupCollection = "groups"
	auditCollection = "poll_action_audit"
)

type Meet struct {
	ID                string
	GroupID           string
	Date              string
	VotingComplete    bool
	CandidateVenueIDs []string
	SelectedVenueID   string
	RestaurantID      string
	RestaurantTime    string
	PreviousVenueIDs  []string
}

type RescheduleInput struct {
	VenueID           *string
	VenueIDSet        bool
	RestaurantID      *string
	RestaurantIDSet   bool
	RestaurantTime    *string
	RestaurantTimeSet bool
}

type Service struct {
	client *firestore.Client
}

func New(client *firestore.Client) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("Firestore client is required")
	}
	return &Service{client: client}, nil
}

func (s *Service) Create(ctx context.Context, groupID, date, actorUID string) (Meet, error) {
	if strings.TrimSpace(groupID) == "" || strings.Contains(groupID, "/") || strings.TrimSpace(actorUID) == "" || !validDate(date) {
		return Meet{}, ErrInvalidInput
	}
	groupRef := s.client.Collection(groupCollection).Doc(groupID)
	meetRef := s.client.Collection(meetCollection).NewDoc()
	auditRef := s.client.Collection(auditCollection).NewDoc()
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if _, err := tx.Get(groupRef); err != nil {
			if status.Code(err) == codes.NotFound {
				return ErrGroupNotFound
			}
			return err
		}
		if err := tx.Create(meetRef, map[string]any{
			"date":      date,
			"completed": false,
			"groupId":   groupID,
		}); err != nil {
			return err
		}
		return tx.Create(auditRef, map[string]any{
			"actionType": "create",
			"actorUid":   actorUID,
			"pollId":     meetRef.ID,
			"meetId":     meetRef.ID,
			"groupId":    groupID,
			"pollDate":   date,
			"meetDate":   date,
			"at":         firestore.ServerTimestamp,
		})
	})
	if err != nil {
		return Meet{}, err
	}
	return Meet{ID: meetRef.ID, GroupID: groupID, Date: date}, nil
}

func (s *Service) GroupID(ctx context.Context, meetID string) (string, error) {
	if !validID(meetID) {
		return "", ErrInvalidInput
	}
	doc, err := s.client.Collection(meetCollection).Doc(meetID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return "", ErrMeetNotFound
	}
	if err != nil {
		return "", err
	}
	groupID, _ := doc.Data()["groupId"].(string)
	if groupID == "" {
		return "", ErrInvalidState
	}
	return groupID, nil
}

func (s *Service) Complete(ctx context.Context, meetID, selectedVenueID, restaurantID, restaurantTime, actorUID string) (Meet, error) {
	if !validID(meetID) || !validID(selectedVenueID) || selectedVenueID == "any" || strings.TrimSpace(actorUID) == "" {
		return Meet{}, ErrInvalidInput
	}
	if (restaurantID == "" && restaurantTime != "") || (restaurantID != "" && (!validID(restaurantID) || !validTime(restaurantTime))) {
		return Meet{}, ErrInvalidInput
	}
	meetRef := s.client.Collection(meetCollection).Doc(meetID)
	selectedVenueRef := s.client.Collection("pubs").Doc(selectedVenueID)
	var restaurantRef *firestore.DocumentRef
	if restaurantID != "" {
		restaurantRef = s.client.Collection("pubs").Doc(restaurantID)
	}
	auditRef := s.client.Collection(auditCollection).NewDoc()
	result := Meet{}
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(meetRef)
		if status.Code(err) == codes.NotFound {
			return ErrMeetNotFound
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		if completed, _ := data["completed"].(bool); completed {
			return ErrInvalidState
		}
		groupID, _ := data["groupId"].(string)
		date, _ := data["date"].(string)
		if groupID == "" || date == "" {
			return ErrInvalidState
		}
		venueName, err := readVenueName(tx, selectedVenueRef)
		if err != nil {
			return err
		}
		restaurantName := ""
		if restaurantRef != nil {
			restaurantName, err = readVenueName(tx, restaurantRef)
			if err != nil {
				return err
			}
		}
		updates := []firestore.Update{
			{Path: "completed", Value: true},
			{Path: "selected", Value: selectedVenueID},
			{Path: "completedByUid", Value: actorUID},
		}
		if restaurantID == "" {
			updates = append(updates,
				firestore.Update{Path: "restaurant", Value: firestore.Delete},
				firestore.Update{Path: "restaurant_time", Value: firestore.Delete},
			)
		} else {
			updates = append(updates,
				firestore.Update{Path: "restaurant", Value: restaurantID},
				firestore.Update{Path: "restaurant_time", Value: restaurantTime},
			)
		}
		if err := tx.Update(meetRef, updates); err != nil {
			return err
		}
		audit := map[string]any{
			"actionType": "complete", "actorUid": actorUID,
			"pollId": meetID, "meetId": meetID, "groupId": groupID,
			"pollDate": date, "meetDate": date,
			"selectedVenueId": selectedVenueID, "selectedVenueName": venueName,
			"at": firestore.ServerTimestamp,
		}
		if restaurantID != "" {
			audit["restaurantId"] = restaurantID
			audit["restaurantName"] = restaurantName
			audit["restaurantTime"] = restaurantTime
		}
		if err := tx.Create(auditRef, audit); err != nil {
			return err
		}
		updated := make(map[string]any, len(data)+4)
		for key, value := range data {
			updated[key] = value
		}
		updated["completed"] = true
		updated["selected"] = selectedVenueID
		updated["completedByUid"] = actorUID
		if restaurantID == "" {
			delete(updated, "restaurant")
			delete(updated, "restaurant_time")
		} else {
			updated["restaurant"] = restaurantID
			updated["restaurant_time"] = restaurantTime
		}
		result = meetFromData(meetID, updated)
		return nil
	})
	if err != nil {
		return Meet{}, err
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, meetID, actorUID string) error {
	if !validID(meetID) || strings.TrimSpace(actorUID) == "" {
		return ErrInvalidInput
	}
	meetRef := s.client.Collection(meetCollection).Doc(meetID)
	auditRef := s.client.Collection(auditCollection).NewDoc()
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(meetRef)
		if status.Code(err) == codes.NotFound {
			return ErrMeetNotFound
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		groupID, _ := data["groupId"].(string)
		date, _ := data["date"].(string)
		if groupID == "" || date == "" {
			return ErrInvalidState
		}
		for _, ref := range []*firestore.DocumentRef{
			meetRef,
			s.client.Collection("votes").Doc(meetID),
			s.client.Collection("attendance").Doc(meetID),
			s.client.Collection("open_actions").Doc(meetID),
		} {
			if err := tx.Delete(ref); err != nil {
				return err
			}
		}
		return tx.Create(auditRef, map[string]any{
			"actionType": "delete", "actorUid": actorUID,
			"pollId": meetID, "meetId": meetID, "groupId": groupID,
			"pollDate": date, "meetDate": date,
			"at": firestore.ServerTimestamp,
		})
	})
}

func (s *Service) Reschedule(ctx context.Context, meetID string, input RescheduleInput, actorUID string) (Meet, error) {
	if !validID(meetID) || strings.TrimSpace(actorUID) == "" || (!input.VenueIDSet && !input.RestaurantIDSet && !input.RestaurantTimeSet) {
		return Meet{}, ErrInvalidInput
	}
	if input.VenueIDSet && (input.VenueID == nil || !validID(*input.VenueID) || *input.VenueID == "any") {
		return Meet{}, ErrInvalidInput
	}
	if input.RestaurantIDSet && input.RestaurantID != nil && !validID(*input.RestaurantID) {
		return Meet{}, ErrInvalidInput
	}
	if input.RestaurantTimeSet && input.RestaurantTime != nil && !validTime(*input.RestaurantTime) {
		return Meet{}, ErrInvalidInput
	}
	meetRef := s.client.Collection(meetCollection).Doc(meetID)
	auditRef := s.client.Collection(auditCollection).NewDoc()
	result := Meet{}
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(meetRef)
		if status.Code(err) == codes.NotFound {
			return ErrMeetNotFound
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		if completed, _ := data["completed"].(bool); !completed {
			return ErrInvalidState
		}
		groupID, _ := data["groupId"].(string)
		date, _ := data["date"].(string)
		oldVenueID, _ := data["selected"].(string)
		oldRestaurantID, _ := data["restaurant"].(string)
		oldRestaurantTime, _ := data["restaurant_time"].(string)
		if groupID == "" || date == "" || oldVenueID == "" || oldVenueID == "any" {
			return ErrInvalidState
		}
		if (oldRestaurantID == "") != (oldRestaurantTime == "") || (oldRestaurantTime != "" && !validTime(oldRestaurantTime)) {
			return ErrInvalidState
		}
		newVenueID := oldVenueID
		if input.VenueIDSet {
			newVenueID = *input.VenueID
		}
		newRestaurantID, newRestaurantTime := oldRestaurantID, oldRestaurantTime
		if input.RestaurantIDSet {
			if input.RestaurantID == nil {
				newRestaurantID, newRestaurantTime = "", ""
			} else {
				newRestaurantID = *input.RestaurantID
			}
		}
		if input.RestaurantTimeSet {
			if input.RestaurantTime == nil {
				newRestaurantTime = ""
			} else {
				newRestaurantTime = *input.RestaurantTime
			}
		}
		if (newRestaurantID == "") != (newRestaurantTime == "") || (newRestaurantTime != "" && !validTime(newRestaurantTime)) {
			return ErrInvalidInput
		}
		venueChanged := newVenueID != oldVenueID
		restaurantChanged := newRestaurantID != oldRestaurantID || newRestaurantTime != oldRestaurantTime
		if !venueChanged && !restaurantChanged {
			result = meetFromData(meetID, data)
			return nil
		}
		venueName, restaurantName := "", ""
		if venueChanged {
			venueName, err = readVenueName(tx, s.client.Collection("pubs").Doc(newVenueID))
			if err != nil {
				return err
			}
		}
		if newRestaurantID != "" && newRestaurantID != oldRestaurantID {
			restaurantName, err = readVenueName(tx, s.client.Collection("pubs").Doc(newRestaurantID))
			if err != nil {
				return err
			}
		}
		updates := []firestore.Update{{Path: "completedByUid", Value: firestore.Delete}}
		updated := make(map[string]any, len(data)+3)
		for key, value := range data {
			updated[key] = value
		}
		if venueChanged {
			history, ok := readStringSlice(data["previous_pubs"])
			if !ok {
				return ErrInvalidState
			}
			history = append(history, oldVenueID)
			updates = append(updates, firestore.Update{Path: "selected", Value: newVenueID}, firestore.Update{Path: "previous_pubs", Value: history})
			updated["selected"], updated["previous_pubs"] = newVenueID, history
		}
		if restaurantChanged {
			if newRestaurantID == "" {
				updates = append(updates,
					firestore.Update{Path: "restaurant", Value: firestore.Delete},
					firestore.Update{Path: "restaurant_time", Value: firestore.Delete},
				)
				delete(updated, "restaurant")
				delete(updated, "restaurant_time")
			} else {
				updates = append(updates,
					firestore.Update{Path: "restaurant", Value: newRestaurantID},
					firestore.Update{Path: "restaurant_time", Value: newRestaurantTime},
				)
				updated["restaurant"], updated["restaurant_time"] = newRestaurantID, newRestaurantTime
			}
		}
		if err := tx.Update(meetRef, updates); err != nil {
			return err
		}
		audit := map[string]any{
			"actionType": "reschedule", "actorUid": actorUID,
			"pollId": meetID, "meetId": meetID, "groupId": groupID,
			"pollDate": date, "meetDate": date,
			"at": firestore.ServerTimestamp,
		}
		if venueChanged {
			audit["previousSelectedVenueId"] = oldVenueID
			audit["selectedVenueId"] = newVenueID
			audit["selectedVenueName"] = venueName
		}
		if restaurantChanged {
			audit["previousRestaurantId"] = oldRestaurantID
			audit["restaurantId"] = newRestaurantID
			audit["restaurantTime"] = newRestaurantTime
			if restaurantName != "" {
				audit["restaurantName"] = restaurantName
			}
		}
		if err := tx.Create(auditRef, audit); err != nil {
			return err
		}
		result = meetFromData(meetID, updated)
		return nil
	})
	if err != nil {
		return Meet{}, err
	}
	return result, nil
}

func readStringSlice(value any) ([]string, bool) {
	switch values := value.(type) {
	case nil:
		return []string{}, true
	case []string:
		return slices.Clone(values), true
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			item, ok := value.(string)
			if !ok {
				return nil, false
			}
			result = append(result, item)
		}
		return result, true
	default:
		return nil, false
	}
}

func readVenueName(tx *firestore.Transaction, ref *firestore.DocumentRef) (string, error) {
	doc, err := tx.Get(ref)
	if status.Code(err) == codes.NotFound {
		return "", ErrVenueNotFound
	}
	if err != nil {
		return "", err
	}
	data := doc.Data()
	if deleted, exists := data["deleted_on"]; exists && deleted != nil {
		return "", ErrVenueNotFound
	}
	name, _ := data["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", ErrVenueNotFound
	}
	return name, nil
}

func (s *Service) ChangeCandidate(ctx context.Context, meetID, venueID, actorUID string, add bool) (Meet, error) {
	if !validID(meetID) || !validID(venueID) || venueID == "any" || strings.TrimSpace(actorUID) == "" {
		return Meet{}, ErrInvalidInput
	}
	meetRef := s.client.Collection(meetCollection).Doc(meetID)
	venueRef := s.client.Collection("pubs").Doc(venueID)
	auditRef := s.client.Collection(auditCollection).NewDoc()
	result := Meet{}
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(meetRef)
		if status.Code(err) == codes.NotFound {
			return ErrMeetNotFound
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		if completed, _ := data["completed"].(bool); completed {
			return ErrInvalidState
		}
		groupID, _ := data["groupId"].(string)
		date, _ := data["date"].(string)
		if groupID == "" || date == "" {
			return ErrInvalidState
		}
		candidates, _ := data["pubs"].(map[string]any)
		if candidates == nil {
			candidates = map[string]any{}
		}
		_, exists := candidates[venueID]
		if (add && exists) || (!add && !exists) {
			result = meetFromData(meetID, data)
			return nil
		}
		venueName := ""
		if add {
			venue, err := tx.Get(venueRef)
			if status.Code(err) == codes.NotFound {
				return ErrVenueNotFound
			}
			if err != nil {
				return err
			}
			venueData := venue.Data()
			if deleted, exists := venueData["deleted_on"]; exists && deleted != nil {
				return ErrVenueNotFound
			}
			venueName, _ = venueData["name"].(string)
			if strings.TrimSpace(venueName) == "" {
				return ErrVenueNotFound
			}
			candidates[venueID] = map[string]any{"name": venueName}
		} else {
			candidate, _ := candidates[venueID].(map[string]any)
			venueName, _ = candidate["name"].(string)
			delete(candidates, venueID)
		}
		if err := tx.Update(meetRef, []firestore.Update{{Path: "pubs", Value: candidates}}); err != nil {
			return err
		}
		action := "deleteVenue"
		if add {
			action = "addVenue"
		}
		if err := tx.Create(auditRef, map[string]any{
			"actionType":      action,
			"actorUid":        actorUID,
			"pollId":          meetID,
			"meetId":          meetID,
			"groupId":         groupID,
			"pollDate":        date,
			"meetDate":        date,
			"selectedVenueId": venueID,
			"venueName":       venueName,
			"at":              firestore.ServerTimestamp,
		}); err != nil {
			return err
		}
		updated := make(map[string]any, len(data)+1)
		for key, value := range data {
			updated[key] = value
		}
		updated["pubs"] = candidates
		result = meetFromData(meetID, updated)
		return nil
	})
	if err != nil {
		return Meet{}, err
	}
	return result, nil
}

func meetFromData(meetID string, data map[string]any) Meet {
	meet := Meet{ID: meetID}
	meet.GroupID, _ = data["groupId"].(string)
	meet.Date, _ = data["date"].(string)
	meet.VotingComplete, _ = data["completed"].(bool)
	meet.SelectedVenueID, _ = data["selected"].(string)
	meet.RestaurantID, _ = data["restaurant"].(string)
	meet.RestaurantTime, _ = data["restaurant_time"].(string)
	meet.PreviousVenueIDs, _ = readStringSlice(data["previous_pubs"])
	if candidates, ok := data["pubs"].(map[string]any); ok {
		meet.CandidateVenueIDs = make([]string, 0, len(candidates))
		for venueID := range candidates {
			meet.CandidateVenueIDs = append(meet.CandidateVenueIDs, venueID)
		}
		slices.Sort(meet.CandidateVenueIDs)
	}
	return meet
}

func validID(value string) bool {
	return strings.TrimSpace(value) != "" && value == strings.TrimSpace(value) && !strings.Contains(value, "/")
}

func validDate(value string) bool {
	date, err := time.Parse(time.DateOnly, value)
	return err == nil && date.Format(time.DateOnly) == value
}

func validTime(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}
