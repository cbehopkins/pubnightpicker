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
	ErrGroupNotFound = errors.New("group not found")
	ErrMeetNotFound  = errors.New("meet not found")
	ErrVenueNotFound = errors.New("venue not found")
	ErrInvalidState  = errors.New("invalid Meet state")
)

const (
	meetCollection  = "polls"
	groupCollection = "groups"
	auditCollection = "poll_action_audit"
)

type Meet struct {
	ID                   string
	GroupID              string
	Date                 string
	VotingComplete       bool
	CandidateVenueIDs    []string
	SelectedVenueID      string
	SelectedVenue        *VenueSummary
	SelectedVenueMissing bool
	RestaurantID         string
	RestaurantTime       string
	PreviousVenueIDs     []string
}

type VoteResult struct {
	UpdateTime *time.Time
}

type VenueSummary struct {
	ID         string
	Status     string
	Name       string
	Address    string
	WebsiteURL string
	ImageURL   string
	VenueType  string
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
	votes  voteRepository
}

type voteRepository interface {
	ChangeVote(context.Context, string, string, string, bool) (absentDelete bool, err error)
	Revision(context.Context, string) (*time.Time, error)
}

func New(client *firestore.Client) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &Service{client: client, votes: &firestoreVoteRepository{client: client}}, nil
}

func (s *Service) Create(ctx context.Context, groupID, date, actorUID string) (Meet, error) {
	if strings.TrimSpace(groupID) == "" || strings.Contains(groupID, "/") || strings.TrimSpace(actorUID) == "" || !validDate(date) {
		return Meet{}, ErrInvalidInput
	}
	groupRef := s.client.Collection(groupCollection).Doc(groupID)
	meetRef := s.client.Collection(meetCollection).NewDoc()
	votesRef := s.client.Collection("votes").Doc(meetRef.ID)
	attendanceRef := s.client.Collection("attendance").Doc(meetRef.ID)
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
		if err := tx.Create(votesRef, map[string]any{}); err != nil {
			return err
		}
		if err := tx.Create(attendanceRef, map[string]any{}); err != nil {
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
		selectedVenue, missingVenue, err := readVenueSummary(tx, selectedVenueRef, false)
		if err != nil {
			return err
		}
		if missingVenue {
			return ErrVenueNotFound
		}
		venueName := selectedVenue.Name
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
		result.SelectedVenue = &selectedVenue
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
		selectedVenue, missingVenue, err := readVenueSummary(tx, s.client.Collection("pubs").Doc(newVenueID), !venueChanged)
		if err != nil {
			return err
		}
		if !venueChanged && !restaurantChanged {
			result = meetFromData(meetID, data)
			setSelectedVenue(&result, selectedVenue, missingVenue)
			return nil
		}
		venueName, restaurantName := selectedVenue.Name, ""
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
		setSelectedVenue(&result, selectedVenue, missingVenue)
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

func readVenueSummary(tx *firestore.Transaction, ref *firestore.DocumentRef, allowDeleted bool) (VenueSummary, bool, error) {
	doc, err := tx.Get(ref)
	if status.Code(err) == codes.NotFound {
		return VenueSummary{}, true, nil
	}
	if err != nil {
		return VenueSummary{}, false, err
	}
	data := doc.Data()
	venueStatus := "active"
	if deleted, exists := data["deleted_on"]; exists && deleted != nil {
		if !allowDeleted {
			return VenueSummary{}, false, ErrVenueNotFound
		}
		venueStatus = "deleted"
	}
	name, _ := data["name"].(string)
	if strings.TrimSpace(name) == "" {
		return VenueSummary{}, false, ErrVenueNotFound
	}
	venueType, _ := data["venueType"].(string)
	if venueType == "" {
		venueType = "pub"
	}
	return VenueSummary{
		ID: ref.ID, Status: venueStatus, Name: name,
		Address: firestoreString(data, "address"), WebsiteURL: firestoreString(data, "web_site"),
		ImageURL: firestoreString(data, "pubImage"), VenueType: venueType,
	}, false, nil
}

func readVenueName(tx *firestore.Transaction, ref *firestore.DocumentRef) (string, error) {
	venue, missing, err := readVenueSummary(tx, ref, false)
	if err != nil {
		return "", err
	}
	if missing {
		return "", ErrVenueNotFound
	}
	return venue.Name, nil
}

func firestoreString(data map[string]any, field string) string {
	value, _ := data[field].(string)
	return value
}

func setSelectedVenue(meet *Meet, venue VenueSummary, missing bool) {
	meet.SelectedVenueMissing = missing
	if !missing {
		meet.SelectedVenue = &venue
	}
}

func (s *Service) ChangeVote(ctx context.Context, meetID, venueID, voterUID string, add bool) (VoteResult, error) {
	if !validID(meetID) || !validID(venueID) || strings.TrimSpace(voterUID) == "" {
		return VoteResult{}, ErrInvalidInput
	}
	absentDelete, err := s.votes.ChangeVote(ctx, meetID, venueID, voterUID, add)
	if err != nil {
		return VoteResult{}, err
	}
	if absentDelete {
		return VoteResult{}, nil
	}
	revision, err := s.votes.Revision(ctx, meetID)
	if err != nil {
		return VoteResult{}, err
	}
	return VoteResult{UpdateTime: revision}, nil
}

func (s *Service) SetAttendance(ctx context.Context, meetID, venueID, uid, attendanceStatus string) error {
	if !validID(meetID) || !validID(venueID) || venueID == "any" || strings.TrimSpace(uid) == "" ||
		(attendanceStatus != "attending" && attendanceStatus != "not_attending") {
		return ErrInvalidInput
	}
	return s.changeAttendance(ctx, meetID, venueID, uid, attendanceStatus, false)
}

func (s *Service) ClearAttendance(ctx context.Context, meetID, venueID, uid string) error {
	if !validID(meetID) || !validID(venueID) || venueID == "any" || strings.TrimSpace(uid) == "" {
		return ErrInvalidInput
	}
	return s.changeAttendance(ctx, meetID, venueID, uid, "", true)
}

func (s *Service) changeAttendance(ctx context.Context, meetID, venueID, uid, attendanceStatus string, clear bool) error {
	meetRef := s.client.Collection(meetCollection).Doc(meetID)
	attendanceRef := s.client.Collection("attendance").Doc(meetID)
	// FIXME there' a lot of work going on in this travnsaction - consider other approaches
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		meetSnapshot, err := tx.Get(meetRef)
		if status.Code(err) == codes.NotFound {
			return ErrMeetNotFound
		}
		if err != nil {
			return err
		}
		meetData := meetSnapshot.Data()
		attendanceSnapshot, err := tx.Get(attendanceRef)
		attendanceExists := err == nil
		if status.Code(err) == codes.NotFound {
			err = nil
		}
		if err != nil {
			return err
		}
		attendanceData := map[string]any{}
		if attendanceExists {
			attendanceData = attendanceSnapshot.Data()
		}
		rawVenueData, venueEntryExists := attendanceData[venueID]
		venueData, hasVenueResponse := attendanceMap(rawVenueData)
		if venueEntryExists && !hasVenueResponse {
			return ErrInvalidState
		}
		canCome, valid := attendanceUsers(venueData["canCome"])
		if !valid {
			return ErrInvalidState
		}
		cannotCome, valid := attendanceUsers(venueData["cannotCome"])
		if !valid {
			return ErrInvalidState
		}
		hasOwnResponse := slices.Contains(canCome, uid) || slices.Contains(cannotCome, uid)
		venueAllowed := attendanceVenueAllowed(meetData, venueID)
		if !venueAllowed && clear {
			venueAllowed = hasOwnResponse
			if !venueAllowed {
				venueAllowed, err = attendanceVenueWasRemoved(ctx, tx, s.client, meetID, venueID, meetData)
				if err != nil {
					return err
				}
			}
		}
		if !venueAllowed {
			return ErrVenueNotFound
		}
		if !hasVenueResponse {
			venueData = make(map[string]any)
		}
		if clear {
			canCome = removeAttendanceUser(canCome, uid)
			cannotCome = removeAttendanceUser(cannotCome, uid)
		} else if attendanceStatus == "attending" {
			canCome = addAttendanceUser(canCome, uid)
			cannotCome = removeAttendanceUser(cannotCome, uid)
		} else {
			cannotCome = addAttendanceUser(cannotCome, uid)
			canCome = removeAttendanceUser(canCome, uid)
		}
		venueData["canCome"] = canCome
		venueData["cannotCome"] = cannotCome
		if _, valid := attendanceETAs(venueData["eta"]); !valid {
			return ErrInvalidState
		}
		if clear && !attendanceExists {
			return nil
		}
		if !attendanceExists {
			return tx.Create(attendanceRef, map[string]any{venueID: venueData})
		}
		updates := []firestore.Update{
			{FieldPath: firestore.FieldPath{venueID, "canCome"}, Value: canCome},
			{FieldPath: firestore.FieldPath{venueID, "cannotCome"}, Value: cannotCome},
		}
		if clear || attendanceStatus == "not_attending" {
			updates = append(updates, firestore.Update{
				FieldPath: firestore.FieldPath{venueID, "eta", uid}, Value: firestore.Delete,
			})
		}
		return tx.Update(attendanceRef, updates)
	})
}

func attendanceVenueAllowed(meet map[string]any, venueID string) bool {
	if completed, _ := meet["completed"].(bool); !completed {
		if candidates, ok := meet["pubs"].(map[string]any); ok {
			if _, exists := candidates[venueID]; exists {
				return true
			}
		}
		return false
	}
	selected, _ := meet["selected"].(string)
	restaurant, _ := meet["restaurant"].(string)
	return venueID == selected || venueID == restaurant
}

func attendanceVenueWasRemoved(ctx context.Context, tx *firestore.Transaction, client *firestore.Client, meetID, venueID string, meet map[string]any) (bool, error) {
	previousVenues, valid := readStringSlice(meet["previous_pubs"])
	if !valid {
		return false, ErrInvalidState
	}
	if slices.Contains(previousVenues, venueID) {
		return true, nil
	}
	query := client.Collection(auditCollection).Where("pollId", "==", meetID)
	documents, err := tx.Documents(query).GetAll()
	if err != nil {
		return false, err
	}
	for _, document := range documents {
		data := document.Data()
		actionType, _ := data["actionType"].(string)
		removedVenueID, _ := data["selectedVenueId"].(string)
		if actionType == "deleteVenue" && removedVenueID == venueID {
			return true, nil
		}
	}
	return false, nil
}

func attendanceMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	data, ok := value.(map[string]any)
	return data, ok
}

func attendanceUsers(value any) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	return readStringSlice(value)
}

func attendanceETAs(value any) (map[string]any, bool) {
	if value == nil {
		return map[string]any{}, true
	}
	etas, ok := value.(map[string]any)
	if ok {
		copy := make(map[string]any, len(etas))
		for uid, eta := range etas {
			copy[uid] = eta
		}
		return copy, true
	}
	return nil, false
}

func addAttendanceUser(users []string, uid string) []string {
	filtered := removeAttendanceUser(users, uid)
	return append(filtered, uid)
}

func removeAttendanceUser(users []string, uid string) []string {
	filtered := make([]string, 0, len(users))
	for _, user := range users {
		if user != uid {
			filtered = append(filtered, user)
		}
	}
	return filtered
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
