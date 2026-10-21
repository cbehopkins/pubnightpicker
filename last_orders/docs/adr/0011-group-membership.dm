# ADR-011 — Group Model, Membership, Visibility, Permissions and Configuration

**Status:** Proposed — decisions captured; open items listed  
**Date:** 2026-10-16  
**Project:** PubNightPicker  
**Scope:** Group identity, discovery, membership, permissions, configuration and lifecycle

## 1. Context

PubNightPicker needs a first-class Group concept to define a community boundary for Meets, members, permissions, defaults and notifications.

The frontend must interact with Groups through the Go HTTP API. It must not read or write Firestore directly, nor depend on Firestore collection names or document layouts.

The design should be simple enough for V1 while leaving room for future growth without forcing premature complexity.

## 2. Decision summary

- Group is a first-class API resource with an opaque, stable ID.
- Group visibility is `public` or `private`.
- Group names are globally unique and case-insensitive.
- Only global administrators can create Groups in V1.
- Group creation atomically records the creator, adds them as a member and grants group-administrator permission.
- Membership is authoritative on the Group, not on a user-side list.
- Public Groups may be joined by authenticated users. Private Groups require administrator-managed membership.
- Group administrators manage membership and group-scoped permissions.
- Global administrators inherit group-administrator capabilities without needing an explicit group-admin grant.
- Group configuration contains four typed settings: default Location, automatic Meet creation schedule, automatic Meet completion schedule and mailing-list address.
- V1 has no Group rename or deletion endpoint.
- V2 should prioritise broader Group creation and soft deletion.

## 3. Group resource

A Group represents a community or administrative boundary. It is not a Meet, Venue or voting session.

The public API resource should have a stable, opaque ID. Clients must treat that ID as the Group's identity and must not derive it from the Group name.

An illustrative response:

```json
{
  "id": "group-opaque-id",
  "name": "ampubnight",
  "visibility": "public",
  "created_by": "user-opaque-id",
  "my_permissions": ["manage_members"],
  "config": {
    "default_location_id": "location-opaque-id",
    "automatic_meet_creation_schedule": null,
    "automatic_meet_completion_schedule": null,
    "mailing_list_address": null
  }
}
```

This is illustrative rather than a final OpenAPI schema. Exact schedule structures and the precise public permission-key inventory remain open.

### 3.1 Group names

Group names are globally unique and compared case-insensitively.

For example, `ampubnight` and `AmPubNight` identify the same name for uniqueness purposes.

V1 does not expose a rename endpoint. The internal model should not make renaming impossible, and a future version may introduce it. The stable Group ID remains unchanged if a rename is introduced.

### 3.2 Creation

In V1, Group creation is restricted to global administrators.

Creating a Group must be one atomic logical operation that:

1. Creates the Group.
2. Sets `created_by` to the authenticated creator.
3. Adds the creator to the Group's membership.
4. Grants the creator group-administrator permission.

The implementation must not leave a partially initialised Group if one of these steps fails.

`created_by` is historical provenance. It is not an ongoing source of authority. If the creator later leaves the Group or loses administrator privileges, their creator status does not grant them continuing access.

Broadening Group creation beyond global administrators is a high-priority V2 feature. Until then, initial setup can be performed administratively.

## 4. Visibility and discovery

A Group has one of two visibility values:

- `public`
- `private`

Visibility determines discoverability. It does not, by itself, grant membership or write access.

### 4.1 Discovery rules

`GET /groups` returns Groups discoverable by the caller:

| Caller | Groups visible through discovery |
|---|---|
| Anonymous | Public Groups |
| Authenticated non-member | Public Groups |
| Group member | Public Groups and private Groups they belong to |
| Global administrator | All Groups |

A private Group that the caller is not authorised to discover must return `404 Not Found` on direct lookup. This avoids revealing the existence of private Groups.

The frontend's initial lookup may use:

```http
GET /groups?name=ampubnight
```

After discovery, the frontend should retain and use the returned opaque Group ID for subsequent requests.

The backend must enforce visibility and authorisation. A frontend filter is not a security boundary.

## 5. Membership

Group membership is authoritative on the Group. V1 does not require a separate user-side membership list to be kept in sync.

The initial implementation may store member IDs as part of the Group's persistence representation. If membership volume or update contention later requires it, the persistence implementation can move membership to a separate collection without changing the public API contract.

### 5.1 Membership rules

- Authenticated users may self-join public Groups.
- Private Groups cannot be self-joined; a Group administrator must add the user.
- Invitations may be introduced later.
- Group administrators can add and remove members.
- Members can voluntarily leave a Group.
- Any Group member can view the Group's member list.
- Only Group administrators or global administrators can manage other members.
- Joining a Group does not grant additional permission keys automatically.
- A Group must always have at least one Group administrator.

Removing or demoting the last Group administrator must be rejected unless another administrator is appointed in the same logical operation.

Membership changes and their permission consequences must be handled consistently. Removing a member must not leave stale group-scoped permission assignments behind.

### 5.2 API

```http
GET    /groups/{group_id}/members
PUT    /groups/{group_id}/members/{user_id}
DELETE /groups/{group_id}/members/{user_id}
```

`PUT` adds the target member where the caller is authorised to do so. It can support both an administrator adding a member and an authenticated user joining a public Group, subject to the relevant authorisation rules.

`DELETE` removes the target member, or allows a member to leave their own Group.

Requests must be idempotent where practical: repeating an add or removal should not create duplicate membership or inconsistent permission state.

## 6. Permissions

Group-scoped permissions are separate from global permissions.

The Group model remains permission-oriented: each supported permission key maps to the users explicitly granted that permission.

### 6.1 Global administrator inheritance

Global administrators can perform operations available to Group administrators, in addition to their global privileges.

This inherited authority is evaluated by backend authorisation logic. It does not require inserting an explicit group-administrator grant into every Group's stored permission assignments.

Where an API response exposes `my_permissions`, it reports explicit group-scoped grants only. It does not merge inherited global-administrator capabilities into that list.

The frontend manages global and group-scoped permissions separately.

### 6.2 Granting permissions

- Group administrators and global administrators can grant or revoke group-scoped permissions.
- Users cannot grant permissions to themselves.
- The backend defines and validates the supported permission-key catalogue.
- Arbitrary permission keys must not be accepted.
- New keys can be added as features require.
- Some actions may be authorised directly by membership rather than by a distinct permission key.

Permission-management endpoints should be scoped beneath the Group rather than creating a global permission resource for Group-specific grants. The final route and payload schema should be defined in OpenAPI alongside the implementation.

### 6.3 Leaving or being removed

When a user leaves or is removed from a Group, their group-scoped permission assignments are removed.

If they later rejoin, those assignments are not automatically restored.

Global permissions are unaffected by Group membership changes.

### 6.4 Legacy permission mapping

Existing stored permission identities should be preserved where practical. The Go backend should map legacy storage values to stable, understandable API permission keys.

The full legacy permission inventory and mapping must be verified against the existing implementation before the API schema is finalised. The API must not expose arbitrary legacy storage strings without validation.

## 7. Group configuration

V1 supports exactly four Group configuration settings:

1. `default_location_id`
2. Automatic Meet creation schedule
3. Automatic Meet completion schedule
4. Group mailing-list address for notifications

These are typed, validated settings—not an arbitrary JSON settings bag.

Illustrative shape:

```json
{
  "default_location_id": "location-opaque-id",
  "automatic_meet_creation_schedule": null,
  "automatic_meet_completion_schedule": null,
  "mailing_list_address": null
}
```

The schedule fields are placeholders until their exact schema has been checked against existing behaviour and requirements.

V1 uses `Europe/London` as the Meet date boundary. Per-Group timezone configuration is explicitly deferred to V3 or a similarly distant future iteration.

The API must validate `default_location_id` against an existing, non-deleted Location.

## 8. Group deletion and renaming

V1 has no Group deletion endpoint and no Group rename endpoint.

Soft deletion is a high-priority V2 feature. It should preserve historical Meets and other records that refer to the Group while hiding the deleted Group from ordinary discovery and operational workflows.

A future rename operation must preserve the Group ID and update the name uniqueness constraint atomically.

## 9. Authorisation principles

The backend is the authority for Group visibility, membership and permissions.

The following principles apply throughout the API:

- Authentication establishes caller identity; it does not automatically grant access.
- Public visibility does not imply membership.
- Group membership does not automatically imply permission to create or complete Meets.
- Global administrator privileges are evaluated by backend policy.
- Private Group existence is hidden from unauthorised callers.
- Frontend checks are for user experience only; the backend must independently enforce every restriction.

## 10. Implementation guidance

Keep the domain model independent of Firestore's storage layout.

Use distinct types for API DTOs, domain objects and persistence records. Repository code should translate legacy Firestore fields and permission identities into domain representations.

Existing Firestore document IDs may initially be exposed as opaque stable resource IDs, provided they are not treated as a client-facing schema.

Group creation, membership updates and permission cleanup must be atomic at the logical level. Where Firestore transactions or batched writes are required, the implementation should explicitly account for their limits and concurrency behaviour.

## 11. Open questions

The following items must be resolved before or during implementation:

1. **Schedule schemas:** Exact typed structures for automatic Meet creation and completion, including timezone handling, recurrence and failure behaviour.
2. **Permission inventory:** Complete legacy permission inventory and stable API-key mapping.
3. **Permission routes:** Exact HTTP routes and request/response schemas for granting and revoking group-scoped permissions.
4. **Membership concurrency:** Transaction and retry strategy for enforcing the last-administrator invariant under concurrent changes.
5. **Group pagination:** Whether Group discovery needs pagination in V1 and, if so, the standard cursor contract.
6. **Group soft deletion:** Detailed V2 behaviour, including how existing Meets and permissions are treated.
7. **Rename policy:** Whether and how a future Group rename affects lookup by name, notification addresses or external integrations.

## 12. Consequences

### Positive

- Groups have a stable identity independent of their name.
- Membership and permissions have a clear authority boundary.
- Public discovery can coexist with private Groups.
- Global administration does not require duplicated per-Group grants.
- The model supports future changes without exposing Firestore layout to clients.

### Trade-offs

- V1 Group creation requires administrative setup.
- Embedded membership is simple but may eventually need to move to a separate persistence representation.
- The permission mapping needs deliberate compatibility work during migration.
- Schedule schemas must be finalised before automatic workflows can be implemented reliably.
