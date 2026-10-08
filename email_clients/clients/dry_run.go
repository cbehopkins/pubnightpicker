package clients

import (
	"maps"
	"slices"
	"strings"
)

type DryRunCallback func() bool

const DryRunHeader = "X-Dry-Run"
const NotificationPrefixVariable = "notification_prefix"
const DryRunSubjectPrefix = "[DRY-RUN] "

func PrepareDryRun(email Email, dryRun bool) Email {
	prefix := ""
	if dryRun {
		prefix = DryRunSubjectPrefix
		if strings.TrimSpace(email.Subject) != "" && !strings.HasPrefix(email.Subject, prefix) {
			email.Subject = prefix + email.Subject
		}
		email.Headers = maps.Clone(email.Headers)
		if email.Headers == nil {
			email.Headers = make(map[string]string)
		}
		for name := range email.Headers {
			if strings.EqualFold(name, DryRunHeader) {
				delete(email.Headers, name)
			}
		}
		email.Headers[DryRunHeader] = "true"
	}
	if email.TemplateID != "" {
		email.Variables = maps.Clone(email.Variables)
		if email.Variables == nil {
			email.Variables = make(map[string]any)
		}
		email.Variables[NotificationPrefixVariable] = prefix
		email.To = slices.Clone(email.To)
		for index := range email.To {
			email.To[index].Variables = maps.Clone(email.To[index].Variables)
			delete(email.To[index].Variables, NotificationPrefixVariable)
		}
	}
	return email
}
