@last_orders @application_time
Feature: Application-time deadline control
  As an E2E author
  I want to advance the backend clock through its local control endpoint
  So that scheduled completion runs through the normal backend pipeline

  Scenario: Voting stays open until the London 16:00 deadline
    Given a fresh emulator database has been bootstrapped with the journey admin
    And the application-time processing floor matches the supplied London date
    And startup autocomplete discovery has settled before fixture creation
    And the following journey venues are available:
      | name             | type |
      | Journey Red Lion | pub  |
      | Journey Crown    | pub  |
    When Bob creates an account
    Then Bob is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Bob the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
    And Bob opens Active Polls
    And Bob creates a poll for the frozen application date
    And Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    And Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    When the application clock advances to "15:59" London time
    Then the application-time poll remains open
    When the application clock advances to "16:00" London time
    Then Last Orders completes the application-time poll at "Journey Red Lion"

  Scenario: Tied voting remains open for manual completion after the deadline
    Given a fresh emulator database has been bootstrapped with the journey admin
    And the application-time processing floor matches the supplied London date
    And startup autocomplete discovery has settled before fixture creation
    And the following journey venues are available:
      | name             | type |
      | Journey Red Lion | pub  |
      | Journey Crown    | pub  |
    When Bob creates an account
    Then Bob is logged in
    When Claire creates an account
    Then Claire is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Bob the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
    And the admin grants Claire the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
    And Bob opens Active Polls
    And Claire opens Active Polls
    And Bob creates a poll for the frozen application date
    And Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    When Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    And Claire votes for the following pubs:
      | venue          |
      | Journey Crown  |
    When the application clock advances to "16:00" London time
    Then Last Orders leaves the application-time poll for manual completion due to a tie

  Scenario: Catch-up leaves the destination London date open until 16:00
    Given a fresh emulator database has been bootstrapped with the journey admin
    And the application-time processing floor matches the supplied London date
    And startup autocomplete discovery has settled before fixture creation
    And the following journey venues are available:
      | name             | type |
      | Journey Red Lion | pub  |
      | Journey Crown    | pub  |
    When Bob creates an account
    Then Bob is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Bob the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
    And Bob opens Active Polls
    When Bob creates a poll for application-time day -1
    And Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    And Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    When Bob creates a poll for application-time day 0
    And Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    And Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    When Bob creates a poll for application-time day 1
    And Bob filters the venue picker to Pub and adds:
      | venue            |
      | Journey Red Lion |
      | Journey Crown    |
    And Bob votes for the following pubs:
      | venue            |
      | Journey Red Lion |
    Then the application-time destination London date is day 1
    When the application clock advances to "15:59" London time
    Then Last Orders completes application-time day -1 at "Journey Red Lion"
    And Last Orders completes application-time day 0 at "Journey Red Lion"
    And the application-time poll for day 1 remains open
    When the application clock advances to "16:00" London time
    Then Last Orders completes application-time day 1 at "Journey Red Lion"
