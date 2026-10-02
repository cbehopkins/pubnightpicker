package polls

import (
	"strings"

	"last_orders/internal/lastorders/components/venuecache"
)

const (
	pollCompletedMailingList = "ampubnight@googlegroups.com"

	completionBase = `Every week we have a pub night to which you are cordially invited.
The venue changes each week, though we do tend to frequent a few favourites.
Suggestions for venues are always welcome.
The earliest attendees get there between 6:30 & 7:30pm and we continue through to closing time.
`
	completionPub = `This week on {{event_date}} we will be visiting {{venue_name}}`

	completionEvent = `Every week we have a pub night to which you are cordially invited.
This week the destination is an event venue.

On {{event_date}} we will be attending {{venue_name}}.`

	completionRestaurant = `Every week we have a pub night to which you are cordially invited.
This week the destination is a restaurant.

On {{event_date}} we will be visiting {{venue_name}}.`

	completionRescheduled = "This week's event has been rescheduled\n\n"
	completionUnsubscribe = "\n\nUnsubscribe at "
)

// completionContent is a provider-variable template plus its common variables.
type completionContent struct {
	Subject   string
	Text      string
	Variables map[string]any
}

// newCompletionContent mirrors firebase_sub send_email.build_notification_text.
func newCompletionContent(venue venuecache.VenueProjection, restaurant *venuecache.VenueProjection, eventDate, restaurantTime, baseURL string, rescheduled, personal bool) completionContent {
	variables := map[string]any{"venue_name": venue.Name, "event_date": eventDate}
	var text strings.Builder
	if rescheduled {
		text.WriteString(completionRescheduled)
	}

	switch venueType(venue) {
	case "event":
		text.WriteString(completionEvent)
	case "restaurant":
		text.WriteString(completionRestaurant)
	default:
		text.WriteString(completionBase)
		if restaurant != nil {
			variables["restaurant_name"] = restaurant.Name
			text.WriteString("\nBefore the pub we are meeting at {{restaurant_name}}.")
			if restaurantTime != "" {
				variables["restaurant_time"] = restaurantTime
				text.WriteString(" We will be meeting there at {{restaurant_time}}.")
			}
			writeVenueDetails(&text, variables, "restaurant", *restaurant)
			text.WriteString("\n\n")
		}
		text.WriteString(completionPub)
	}
	writeVenueDetails(&text, variables, "venue", venue)
	if personal {
		text.WriteString(completionUnsubscribe)
		text.WriteString(baseURL)
		text.WriteString("/preferences/{{uid}}")
	}

	subject := "Pub Night @ {{venue_name}}"
	if rescheduled {
		subject = "Pub Night @ RESCHEDULED::{{venue_name}}"
	}
	return completionContent{Subject: subject, Text: text.String(), Variables: variables}
}

func writeVenueDetails(text *strings.Builder, variables map[string]any, prefix string, venue venuecache.VenueProjection) {
	siteLabel, mapLabel := "Pub Web Site", "Map to pub"
	switch venueType(venue) {
	case "event":
		siteLabel, mapLabel = "Event Web Site", "Map to event"
	case "restaurant":
		siteLabel, mapLabel = "Restaurant Web Site", "Map to restaurant"
	}
	if venue.Website != "" {
		variables[prefix+"_website"] = venue.Website
		text.WriteString("\n")
		text.WriteString(siteLabel)
		text.WriteString(" {{")
		text.WriteString(prefix)
		text.WriteString("_website}}\n")
	}
	if venue.Address != "" {
		variables[prefix+"_address"] = venue.Address
		text.WriteString("\n{{")
		text.WriteString(prefix)
		text.WriteString("_address}}\n")
	}
	if venue.Map != "" {
		variables[prefix+"_map"] = venue.Map
		text.WriteString("\n\n")
		text.WriteString(mapLabel)
		text.WriteString(" {{")
		text.WriteString(prefix)
		text.WriteString("_map}}")
	}
}

// venueType treats a missing type as a pub, matching the stored venue contract.
func venueType(venue venuecache.VenueProjection) string {
	if venue.VenueType == "" {
		return "pub"
	}
	return venue.VenueType
}
