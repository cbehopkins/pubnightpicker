Feature: Members organise and chat about a meet
  As members organising a pub night
  We want to create a poll, vote and choose a pub
  So that everyone can see the agreed meet

  Scenario: Bob and Claire organise tomorrow's meet and exchange chat messages
    Given a fresh emulator database has been bootstrapped with the journey admin
    And the following journey venues are available:
      | name             | type       |
      | Journey Red Lion | pub        |
      | Journey Crown    | pub        |
      | Journey Bistro   | restaurant |
    When Bob creates an account
    Then Bob is logged in
    When Claire creates an account
    Then Claire is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Bob the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
      | Can Chat                 |
    And the admin grants Claire the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
      | Can Complete/Reschedule  |
      | Can Chat                 |
    And Claire opens Active Polls
    And Bob creates a poll for tomorrow
    Then Claire sees tomorrow's poll on Active Polls
    When Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    Then Claire sees the following pubs in tomorrow's poll:
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
    When Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    Then Claire sees the following vote totals:
      | venue            | votes |
      | Journey Red Lion | 2     |
      | Journey Crown    | 1     |
    When Claire completes tomorrow's poll by choosing "Journey Red Lion"
    Then Claire no longer sees tomorrow's poll on Active Polls
    And Bob no longer sees tomorrow's poll on Active Polls
    When Claire opens Current Events
    Then Claire sees tomorrow's meet at "Journey Red Lion"
    When Bob opens Current Events
    Then Bob sees tomorrow's meet at "Journey Red Lion"
    When Bob opens Chat Page
    And Claire opens Chat Page
    And Bob sends a global chat message
    Then Claire sees the global chat message
    When Claire opens Event Chat for "Journey Red Lion"
    And Bob opens Event Chat for "Journey Red Lion"
    And Claire sends an event chat message
    Then Bob sees the event chat message
    And Bob does not see the global chat message
    When Bob opens Event Chat for "Journey Red Lion"
    Then Bob sees the event chat message
    And Bob does not see the global chat message
    When Bob opens Chat Page
    Then Bob sees the global chat message
    And Bob does not see the event chat message
