package truths

import (
	"time"

	"cellar/pkg/cellar"
)

const AdminDeleteRequestedFanout cellar.HandlerName = "truths.admin_delete_requested"
const AdminDeletePreviewFanout cellar.HandlerName = "truths.admin_delete_preview"

var AdminDeleteRequestedRegistry = NewRegistry[AdminDeleteRequested](AdminDeleteRequestedFanout)
var AdminDeletePreviewRegistry = NewRegistry[AdminDeleteRequested](AdminDeletePreviewFanout)

type AdminDeleteRequestSnapshot struct {
	RequestID        string    `json:"request_id"`
	TargetUID        string    `json:"target_uid"`
	TargetEmail      string    `json:"target_email"`
	RequestedByUID   string    `json:"requested_by_uid"`
	RequestedByEmail string    `json:"requested_by_email"`
	Reason           string    `json:"reason"`
	SchemaVersion    int       `json:"schema_version"`
	CreatedAt        time.Time `json:"created_at"`
}

type AdminDeleteRequested struct {
	Request AdminDeleteRequestSnapshot `json:"request"`
}

func (truth AdminDeleteRequested) Identity() string {
	return truth.Request.RequestID
}
