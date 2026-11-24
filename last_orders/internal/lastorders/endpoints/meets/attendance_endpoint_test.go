package meets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type attendanceMemberAuthorizerStub func(context.Context, string, string) (bool, error)

func (authorize attendanceMemberAuthorizerStub) IsMember(ctx context.Context, uid, groupID string) (bool, error) {
	return authorize(ctx, uid, groupID)
}

type attendanceWriterStub struct {
	groupID string
	changes []string
}

func (writer *attendanceWriterStub) PatchAttendance(_ context.Context, meetID, venueID, uid, status string, etaPresent bool, eta *string) error {
	etaText := ""
	if etaPresent {
		if eta == nil {
			etaText = "clear"
		} else {
			etaText = *eta
		}
	}
	writer.changes = append(writer.changes, strings.Join([]string{"patch", meetID, venueID, uid, status, etaText}, ":"))
	return nil
}

func (writer *attendanceWriterStub) GroupID(context.Context, string) (string, error) {
	return writer.groupID, nil
}

func (writer *attendanceWriterStub) SetAttendance(_ context.Context, meetID, venueID, uid, status string) error {
	writer.changes = append(writer.changes, strings.Join([]string{"set", meetID, venueID, uid, status}, ":"))
	return nil
}

func (writer *attendanceWriterStub) ClearAttendance(_ context.Context, meetID, venueID, uid string) error {
	writer.changes = append(writer.changes, strings.Join([]string{"clear", meetID, venueID, uid}, ":"))
	return nil
}

func TestAttendanceMutationsRequireMembershipAndUseVerifiedUID(t *testing.T) {
	writer := &attendanceWriterStub{groupID: "private-group"}
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Members: attendanceMemberAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "private-group", nil
		}),
		Attendance: writer,
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	for _, test := range []struct {
		method  string
		venueID string
		body    string
		want    string
	}{
		{method: http.MethodPut, venueID: "venue-1", body: `{"status":"attending"}`, want: "set:meet-a:venue-1:verified-user:attending"},
		{method: http.MethodPut, venueID: "venue-1", body: `{"status":"not_attending"}`, want: "set:meet-a:venue-1:verified-user:not_attending"},
		{method: http.MethodDelete, venueID: "venue-1", want: "clear:meet-a:venue-1:verified-user"},
		{method: http.MethodPut, venueID: "any", body: `{"status":"attending"}`, want: "set:meet-a:any:verified-user:attending"},
		{method: http.MethodDelete, venueID: "any", want: "clear:meet-a:any:verified-user"},
	} {
		request := httptest.NewRequest(test.method, "/api/meet/meet-a/attendance/"+test.venueID, strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer valid-token")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s status=%d body=%s, want HTTP 204", test.method, response.Code, response.Body)
		}
		if got := writer.changes[len(writer.changes)-1]; got != test.want {
			t.Fatalf("%s mutation=%q, want %q", test.method, got, test.want)
		}
	}
}

func TestAttendancePatchSupportsOmittedStatusAndClearETA(t *testing.T) {
	writer := &attendanceWriterStub{groupID: "private-group"}
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Members: attendanceMemberAuthorizerStub(func(_ context.Context, uid, groupID string) (bool, error) {
			return uid == "verified-user" && groupID == "private-group", nil
		}),
		Attendance: writer,
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	for _, test := range []struct {
		body string
		want string
	}{
		{body: `{"eta":"19:45"}`, want: "patch:meet-a:venue-1:verified-user::19:45"},
		{body: `{"eta":null}`, want: "patch:meet-a:venue-1:verified-user::clear"},
		{body: `{"status":"not_attending","eta":"19:45"}`, want: "patch:meet-a:venue-1:verified-user:not_attending:19:45"},
	} {
		request := httptest.NewRequest(http.MethodPatch, "/api/meet/meet-a/attendance/venue-1", strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer valid-token")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("PATCH status=%d body=%s, want HTTP 204", response.Code, response.Body)
		}
		if got := writer.changes[len(writer.changes)-1]; got != test.want {
			t.Fatalf("PATCH mutation=%q, want %q", got, test.want)
		}
	}
}

func TestAttendanceMutationRejectsNonMemberWithoutCallingWriter(t *testing.T) {
	writer := &attendanceWriterStub{groupID: "public-group"}
	endpoint := &Endpoint{
		Auth: pastMeetAuthStub{},
		Members: attendanceMemberAuthorizerStub(func(context.Context, string, string) (bool, error) {
			return false, nil
		}),
		Attendance: writer,
	}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodPut, "/api/meet/meet-a/attendance/venue-1", strings.NewReader(`{"status":"attending"}`))
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("non-member response status=%d body=%s, want HTTP 403", response.Code, response.Body)
	}
	if len(writer.changes) != 0 {
		t.Fatalf("non-member reached attendance writer: %v", writer.changes)
	}
}

func TestAttendanceSnapshotUnavailableIsNotAnEmptySnapshot(t *testing.T) {
	endpoint := &Endpoint{Auth: pastMeetAuthStub{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/meet/meet-a/attendance", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("attendance response status=%d body=%s, want HTTP 503", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), `"attendance":{}`) {
		t.Fatalf("unavailable attendance was represented as empty: %s", response.Body)
	}
}
