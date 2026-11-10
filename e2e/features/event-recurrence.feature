@last_orders
Feature: Last Orders processes event recurrence
  Background:
    Given a fresh emulator database has been bootstrapped with the journey admin
    And recurrence dates are anchored to today's London calendar date

  Scenario: Another member edits a monthly Event and sees its recalculated date
    When Ada creates an account
    Then Ada is logged in
    When Bea creates an account
    Then Bea is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Ada the following permissions:
      | permission        |
      | Can Manage Venues |
    And the admin grants Bea the following permissions:
      | permission        |
      | Can Manage Venues |
    And Bea opens Manage Venues
    And Ada creates the journey Event with "monthly" recurrence
    And Ada opens the journey venue for editing
    Then Last Orders persists the expected occurrence for the journey Event
    And Ada sees the backend-computed Next Event in the venue form
    When Bea opens the journey venue for editing
    And Bea changes the journey Event to the other monthly date
    Then Last Orders persists the expected occurrence for the journey Event
    When Bea opens the journey venue for editing
    Then Bea sees the backend-computed Next Event in the venue form
    When Ada opens Manage Venues
    And Ada opens the journey venue for editing
    Then Ada sees the backend-computed Next Event in the venue form

  Scenario: A weekly Event due tomorrow becomes an Active Poll
    When Will creates an account
    Then Will is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Will the following permissions:
      | permission             |
      | Can Manage Venues      |
      | Can Create/Delete Poll |
    And Will creates the journey Event with "weekly" recurrence
    And Will opens the journey venue for editing
    Then Last Orders persists the expected occurrence for the journey Event
    And Will sees the backend-computed Next Event in the venue form
    And Last Orders materialises the journey Event poll with votes and attendance
    When Will opens Active Polls
    Then Will sees the generated poll with the journey Event venue