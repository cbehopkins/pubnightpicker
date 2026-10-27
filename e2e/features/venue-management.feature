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
      | Map            | https://maps.example.test/original    |
      | Notes          | Original venue details                |
      | Parking        | false                                 |
      | Food           | <initial food>                        |
    Then <editor> sees the journey venue listed as "<type>"
    When <editor> opens the journey venue for editing
    Then <editor> sees the saved journey venue details
    When <editor> updates the journey venue with these details:
      | Name           | Journey Updated Venue                |
      | Address        | 2 Corrected Road, Cambridge          |
      | Venue Web Site | https://updated.example.test         |
      | Map            | https://maps.example.test/updated    |
      | Notes          | Details corrected by another member |
      | Parking        | true                                 |
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

    Examples:
      | creator | editor | type       | corrected type | initial food |
      | Dana    | Erin   | Pub        | Restaurant     | false        |
      | Fiona   | George | Restaurant | Event          | true         |
      | Helen   | Ivan   | Event      | Pub            | false        |
