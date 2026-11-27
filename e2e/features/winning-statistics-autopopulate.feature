@last_orders
Feature: Winning statistics and autopopulate audit attribution
  As an administrator and a Group member allowed to add candidates
  I want to see accurate Group-scoped winning statistics and auditable autopopulate actions
  So that I can rely on the report and identify who changed a Meet

  Scenario: Admin checks winning statistics and verifies who autopopulated a Meet
    Given a fresh emulator database has been bootstrapped with the journey admin
    And the following journey venues are available:
      | name                   | type | food  |
      | Statistics Crown       | pub  | true  |
      | Statistics Red Lion    | pub  | false |
      | Statistics Zero Winner | pub  | true  |
    When Bob creates an account
    Then Bob is logged in
    When Claire creates an account
    Then Claire is logged in
    And Bob and Claire automatically join the default Group
    When the winning statistics history fixture is seeded for the current Group
    And the journey admin logs in through a separate browser context
    And the admin grants Bob the following permissions:
      | permission               |
      | Can Add/Delete Poll Pubs |
      | Can Create/Delete Poll   |
    When the admin opens winning venue statistics
    Then the admin sees accurate Group-scoped winning statistics
    When Bob creates a poll on a clear date
    Then Bob sees the poll on Active Polls
    When Bob autopopulates the poll
    Then another admin sees the autopopulate audit attributed to Bob
