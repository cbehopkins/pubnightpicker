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
		method string
		body   string
		want   string
	}{
		{method: http.MethodPut, body: `{"status":"attending"}`, want: "set:meet-a:venue-1:verified-user:attending"},
		{method: http.MethodPut, body: `{"status":"not_attending"}`, want: "set:meet-a:venue-1:verified-user:not_attending"},
		{method: http.MethodDelete, want: "clear:meet-a:venue-1:verified-user"},
	} {
		request := httptest.NewRequest(test.method, "/api/meet/meet-a/attendance/venue-1", strings.NewReader(test.body))
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
