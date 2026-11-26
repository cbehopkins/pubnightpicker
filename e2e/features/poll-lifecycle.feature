@last_orders
Feature: Members organise and chat about a meet
  As members organising a pub night
  We want to create a poll, vote and choose a pub
  So that everyone can see the agreed meet

  Scenario: Bob and Claire organise a future meet and exchange chat messages
    Given a fresh emulator database has been bootstrapped with the journey admin
    And the following journey venues are available:
      | name             | type       | food  |
      | Journey Red Lion | pub        | false |
      | Journey Crown    | pub        | true  |
      | Journey Bistro   | restaurant | true  |
    When Bob creates an account
    Then Bob is logged in
    When Claire creates an account
    Then Claire is logged in
    And Bob and Claire automatically join the default Group
    And Bob's existing session repairs missing Group membership on reload
    When the journey admin logs in through a separate browser context
    And the admin grants Bob the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
      | Can Vote                 |
      | Can Chat                 |
    And the admin grants Claire the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
      | Can Vote                 |
      | Can Complete/Reschedule  |
      | Can Chat                 |
    And Claire hides her votes from other members
    And Claire opens Active Polls
    And Bob creates a poll on a clear date
    Then Claire sees the poll on Active Polls
    And the admin sees Bob as the poll creator in the audit
    When Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
    When Bob sets the Any-Venue response to "Can come"
    When Bob filters the venue picker to Pub and adds:
      | venue         |
      | Journey Crown |
    When Bob filters the venue picker to Restaurant and adds:
      | venue          |
      | Journey Bistro |
    Then Bob sees the standing response on "Journey Crown"
    Then Claire sees the following pubs in the poll:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    When Claire votes for the following pubs:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    Then Bob sees the following vote totals:
      | venue            | votes |
      | Journey Red Lion | 1     |
      | Journey Crown    | 1     |
    Then Bob sees the vote through the API without Claire's UID
    When Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    Then Claire sees the following vote totals:
      | venue            | votes |
      | Journey Red Lion | 2     |
      | Journey Crown    | 1     |
    And Claire receives Bob's public voter summary through the API
    And Claire marks "Journey Red Lion" as "Cannot come" while voting
    And Claire marks "Journey Bistro" as "Can come" while voting
    Then the Any-Venue response is stored separately from explicit Venue responses
    And the poll stores ETAs for Bob's standing response and Claire's restaurant response
    When Bob changes the Any-Venue response to "Cannot come"
    Then the Any-Venue response is stored separately from explicit Venue responses
    And the Any-Venue change clears Bob's invalid ETA and preserves Claire's restaurant ETA
    When Bob withdraws the Any-Venue response
    Then the withdrawal leaves explicit Venue responses untouched
    And withdrawing Any-Venue preserves Claire's restaurant ETA
    When Bob sets the Any-Venue response to "Can come"
    When Claire completes the poll by choosing "Journey Red Lion" with restaurant "Journey Bistro"
    Then Claire no longer sees the poll on Active Polls
    And Bob no longer sees the poll on Active Polls
    When admin opens attendance statistics
    Then admin sees the attendance statistics through the API
    When Claire opens Current Meets
    Then Claire sees the meet at "Journey Red Lion"
    When Bob opens Current Meets
    Then Bob sees the meet at "Journey Red Lion"
    When admin opens Current Meets
    Then Claire's browser used the attendance API to load the live view
    Then Bob sees Bob can come to "Journey Red Lion"
    And Claire sees Bob can come to "Journey Red Lion"
    And Claire sees Claire cannot come to "Journey Red Lion"
    And Claire sees Claire can come to "Journey Bistro"
    And Bob has no attendance response at "Journey Bistro"
    When Bob marks "Journey Red Lion" as "Cannot come"
    Then Claire sees Bob cannot come to "Journey Red Lion"
    When Bob withdraws attendance for "Journey Red Lion"
    Then Claire sees Bob can come to "Journey Red Lion"
    When Claire reconnects to Current Meets
    Then Claire sees Bob can come to "Journey Red Lion"
    When Bob adds ETA "18:30" at "Journey Red Lion"
    And Claire marks "Journey Red Lion" as "Can come"
    And Claire adds ETA "19:00" at "Journey Red Lion"
    Then the admin sees "Claire" can come to "Journey Red Lion"
    When Bob marks "Journey Red Lion" as "Cannot come"
    Then the admin sees "Bob" cannot come to "Journey Red Lion"
    And Claire sees Bob cannot come to "Journey Red Lion"
    And changing Bob's response clears only Bob's ETA
    When Bob withdraws attendance for "Journey Red Lion"
    Then Claire sees Bob can come to "Journey Red Lion"
    And Claire still sees "Journey Red Lion" as "Can come confirmed"
    And Bob's browser used the attendance API to set and withdraw responses for "Journey Red Lion"
    When Bob opens Chat Page
    And Claire opens Chat Page
    And Bob sends a global chat message
    Then Claire sees the global chat message
    When Claire opens Meet Chat for "Journey Red Lion"
    And Bob opens Meet Chat for "Journey Red Lion"
    And Claire sends a Meet Chat message
    Then Bob sees the Meet Chat message
    And Bob does not see the global chat message
    When Bob opens Meet Chat for "Journey Red Lion"
    Then Bob sees the Meet Chat message
    And Bob does not see the global chat message
    When Bob opens Chat Page
    Then Bob sees the global chat message
    And Bob does not see the Meet Chat message
