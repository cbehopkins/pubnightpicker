@last_orders
Feature: Authorised members create and edit venues
  As members maintaining the venue catalogue
  We want to edit venues regardless of who created them
  So that venue details and types can be corrected

  Scenario Outline: Another member edits a <type> and corrects its type to <corrected type>
    Given a fresh emulator database has been bootstrapped with the journey admin
    When <creator> creates an account
    Then <creator> is logged in
    When <editor> creates an account
    Then <editor> is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants <creator> the following permissions:
      | permission               |
      | Can Manage Venues        |
      | Can Create/Delete Poll   |
      | Can Add/Delete Poll Pubs |
    And the admin grants <editor> the following permissions:
      | permission        |
      | Can Manage Venues |
    And <editor> opens Manage Venues
    And <creator> creates a "<type>" venue with these details:
      | Name           | Journey Original Venue                |
      | Address        | 1 Original Street, Cambridge          |
      | Venue Web Site | https://original.example.test         |
      | Link to Venue Image | https://images.example.test/original.png |
      | Map            | https://maps.example.test/original    |
      | Notes          | Original venue details                |
      | Parking        | false                                 |
      | Dog Friendly   | true                                  |
      | Beer Garden    | false                                 |
      | Out Of Town (Cambridge) | true                           |
      | Banned         | true                                  |
      | Food           | <initial food>                        |
    Then <editor> sees the journey venue listed as "<type>"
    When <editor> opens the journey venue for editing
    Then <editor> sees the saved journey venue details
    When <editor> updates the journey venue with these details:
      | Name           | Journey Updated Venue                |
      | Address        | 2 Corrected Road, Cambridge          |
      | Venue Web Site | https://updated.example.test         |
      | Link to Venue Image | https://images.example.test/updated.png |
      | Map            | https://maps.example.test/updated    |
      | Notes          | Details corrected by another member |
      | Parking        | true                                 |
      | Dog Friendly   | false                                |
      | Beer Garden    | true                                 |
      | Out Of Town (Cambridge) | false                         |
      | Banned         | true                                 |
      | Food           | true                                 |
    Then <creator> sees the renamed journey venue without reloading
    When <creator> opens the journey venue for editing
    Then <creator> sees the saved journey venue details
    When <creator> opens Manage Venues
    And <editor> opens the journey venue for editing
    And <editor> changes the journey venue type to "<corrected type>"
    Then <creator> no longer sees the journey venue under "<type>"
    And <creator> sees the journey venue listed as "<corrected type>"
    When <creator> opens the journey venue for editing
    Then <creator> sees the saved journey venue details
    When <editor> opens the journey venue for editing
    Then <editor> sees the saved journey venue details
    And <creator> can select the edited journey venue in the poll picker
    And <creator> opens Manage Venues
    When <editor> deletes the journey venue
    Then <creator> no longer sees the journey venue without reloading

    Examples:
      | creator | editor | type       | corrected type | initial food |
      | Dana    | Erin   | Pub        | Restaurant     | false        |
      | Fiona   | George | Restaurant | Event          | true         |
      | Helen   | Ivan   | Event      | Pub            | false        |

  Scenario: Venue managers merge disjoint edits and resolve a field conflict
    Given a fresh emulator database has been bootstrapped with the journey admin
    When Alice creates an account
    Then Alice is logged in
    When Beth creates an account
    Then Beth is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Alice the following permissions:
      | permission        |
      | Can Manage Venues |
    And the admin grants Beth the following permissions:
      | permission        |
      | Can Manage Venues |
    And Alice opens Manage Venues
    And Beth opens Manage Venues
    When Alice creates a "Pub" venue with these details:
      | Name                    | Concurrent Venue          |
      | Address                 | 1 Original Road           |
      | Venue Web Site          | https://original.example.test |
      | Map                     | https://maps.example.test/original |
      | Notes                   | Original notes            |
      | Parking                 | false                     |
      | Dog Friendly            | false                     |
      | Beer Garden             | false                     |
      | Out Of Town (Cambridge) | false                     |
      | Banned                  | false                     |
      | Food                    | false                     |
    Then Beth sees the journey venue listed as "Pub"
    When Alice opens the journey venue for editing
    And Beth opens the journey venue for editing
    When Beth drafts the journey venue field "Address" to "Independent Road"
    And Alice changes the journey venue field "Notes" to "Alice's notes"
    Then Beth sees the journey venue field "Notes" as "Alice's notes"
    And Beth sees the journey venue field "Address" as "Independent Road"
    When Beth changes the journey venue field "Address" to "Independent Road"
    And Beth drafts the journey venue field "Name" to "Beth's draft"
    When Alice changes the journey venue field "Name" to "Alice's name"
    Then Beth sees the journey venue field "Name" as "Beth's draft"
    When Beth saves the journey venue
    Then Beth sees a field conflict for "name"
    And Beth sees the journey venue field "Name" as "Beth's draft"
    When Beth loads the current venue after a field conflict
    Then Beth sees the journey venue field "Name" as "Alice's name"
    When Beth retries the journey venue field "Name" to "Beth's final name"
    Then Beth sees the journey venue listed as "Pub"

  Scenario: A manager reviews the venue before retrying a stale deletion
    Given a fresh emulator database has been bootstrapped with the journey admin
    When Alice creates an account
    Then Alice is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Alice the following permissions:
      | permission        |
      | Can Manage Venues |
    And Alice opens Manage Venues
    When Alice creates a "Pub" venue with these details:
      | Name    | Venue For Stale Delete |
      | Address | 1 Original Road        |
      | Venue Web Site | https://original.example.test |
      | Map     | https://maps.example.test/original |
      | Notes   | Original details       |
    Then Alice sees the journey venue listed as "Pub"
    When Alice's delete races with a venue update
    Then Alice reviews the current venue after stale deletion
    When Alice deliberately retries the reviewed deletion
    Then Alice no longer sees the journey venue without reloading
