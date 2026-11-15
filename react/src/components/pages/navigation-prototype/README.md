# Navigation prototype

Throwaway prototype: can participants reach this week's vote or meeting details
immediately, while organisers comfortably manage Meets, Venues and Locations?

## Run

From `react/`, run `npm run dev`, then open
`http://localhost:3000/prototype/navigation?variant=A` (use the port printed by
Vite if 3000 is occupied). This entry is development-only and does not load the
normal application, its custom styles, authentication or Firebase subscriptions.

## Compare

- A: separate participation and organising workspaces.
- B: primary navigation with contextual secondary navigation.
- C: a community-centred sidebar, with organising tools collapsed on mobile.

The floating prototype bar switches variants and participant/organiser/admin
views. Left/right arrow keys also switch variants outside editing controls.
Variants share session state; reloading resets all sample data. The bar reports
the current workflow, vote, attendance, draft selections and changes.

Try voting, closing voting, confirming attendance, finding a past Meet, creating
a Meet, editing a selected venue without losing the Meet draft, and editing a
Location. Chat and account preferences are local demonstrations. Administration
panels are placeholders, not operational tools. Voting results are illustrative,
not an implementation of the real poll completion rules.

## Scope and later comparisons

Use stock React Bootstrap with no existing theme overrides. Compare navigation
structures first; subsequently compare one preferred structure with stock MUI.
No framework migration is implied by this prototype.

A community is a separate community of people, not a geographical area.
Locations geographically group venues. Later iterations should add location
selection defaulting to the community's usual location, then community discovery,
switching and richer community management. A Cambridge community can organise a
Meet in London. Group-specific domains and the all-community entry point are
outside this prototype's initial scope.

Before implementation, record the winning structure, borrowed elements and why.
Preserve the prototype on a throwaway branch only when explicitly authorised;
rewrite the selected design to production standards rather than shipping this
code directly.