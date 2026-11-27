# CDD: Locations and Venue Selection

**Status:** ready-for-agent  
**Date:** 2026-11-27  
**Related:** [HTTP API Architecture](../adr/0012-api-architecture.md); [Group Membership and Configuration](../adr/0011-group-membership.md); [OpenAPI](../pubnightpicker-openapi-v1.yaml)

## 1. Purpose

Add named Locations to group Venues for selection. This replaces the current
practice of relying on local knowledge or adding place names to Venue names
when a Venue is outside the Cambridge area. The feature covers Location
management, Venue association, Group defaults, Poll filtering and eventual
removal of the legacy `out_of_town` field.

The implementation is divided into nine tickets in
`.scratch/locations/issues/`. The feature brief and acceptance detail are in
`.scratch/locations/spec.md`.

## 2. Domain model

A Location has a stable ID, a name and an optional map URL. It groups Venues; it
does not represent a Venue or a Group. Each Venue references one Location by
ID. A Group may optionally reference a default Location, which is a preference
for Poll setup and does not constrain the Venues available to the Group.

Location names are unique across the Location collection after trimming
surrounding whitespace and Unicode case folding. The normalized name `all` is
reserved. Venue names use the same normalization and reserved name, but are
unique only among active Venues in the selected Location. The same Venue name
may appear in different Locations. Tombstoned Venues do not block reuse of a
name. Editing a record excludes that record from its duplicate check.

## 3. Location lifecycle and authorization

Location create, edit and delete operations are available to callers with the
global `canEditLocations` permission and implicitly to site administrators.
Last Orders is the authorization boundary. Public reads omit tombstoned
Locations from ordinary discovery. Deletion sets a tombstone and retains the
Location's ID and details so existing Venue and Group references continue to
resolve. A tombstoned Location cannot be assigned to a Venue or Group.

The Location management page is a top-level tab visible to site administrators
and callers with `canEditLocations`. It supports creating, editing and
tombstoning Locations. E2E coverage exercises all three operations.

## 4. Venue and Group behavior

The Venue contract uses `location_id` as the Location reference. Venue create
and edit flows select from active Locations. The current Last Orders Venue
contract already contains `location_id`, and the Venue list endpoint already
accepts it as a filter; implementation must verify that existing semantics match
the Location reference and retain the filter when combining it with Venue type
and other supported filters.

An admin-only Group properties API allows site administrators to rename a
Group and set or clear its default Location. Rename preserves the Group ID and
updates the existing Group-name uniqueness constraint atomically. A configured
default must identify an active Location.

## 5. Poll selection

Poll setup has a Location selector beside Venue type. It lists active Locations
by name and has a distinct first option, “All locations,” which is a UI
sentinel and never a Location ID. Initially this option is selected. Selecting
a Location filters candidate Venues by its ID through the Venue list API.

A later ticket loads the default Group's default Location and preselects it when
it is active. If the default is unset, missing or tombstoned, Poll setup falls
back to “All locations.” E2E journeys cover the all-Locations view, switching
between Locations with different Venues, and the default selection behavior.

## 6. Data transition

After Location creation and Venue association are available, a manually run
one-off script assigns all existing Venues to the operator-supplied Cambridge
Location ID. The script must not guess the ID, create the Location, or modify
unrelated fields. The migration is deliberately a manual release step.

The `out_of_town` field remains during the transition. A final ticket removes it
from the frontend, API, projections, persistence mapping and data contract once
the backfill is complete. Location references become the sole location
classification.

## 7. Implementation sequence

1. Location collection, CRUD/tombstone API, permission, management UI and E2E.
2. Venue-to-Location association and editing UI.
3. Admin Group property editing for Group name and default Location, with API
   and E2E coverage.
4. Manual Cambridge Location backfill script.
5. Confirm/update Venue list filtering by `location_id`.
6. Poll Location selector and location-specific candidate lists, with E2E.
7. Default the Poll Location from the startup-selected Group, with E2E updates.
8. Frontend and backend normalized-name validation for Locations and Venues.
9. Remove `out_of_town` after backfill.

Each ticket's acceptance criteria are recorded in the corresponding issue file.
