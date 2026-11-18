@last_orders @application_time
Feature: Application-time deadline control
  As an E2E author
  I want to advance the backend clock through its local control endpoint
  So that scheduled completion runs through the normal backend pipeline

  Scenario: Voting stays open until the London 16:00 deadline
    Given a fresh emulator database has been bootstrapped with the journey admin
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