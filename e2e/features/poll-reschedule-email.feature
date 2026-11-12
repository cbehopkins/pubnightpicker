@last_orders
Feature: Last Orders submits eligible poll reschedule emails through the dummy client
  Background:
    Given a fresh emulator database has been bootstrapped with the journey admin
    And notification suppression is off in the fresh emulator
    When Soren creates an account
    Then Soren is logged in
    When Tessa creates an account
    Then Tessa is logged in
    When Uma creates an account
    Then Uma is logged in
    When Vera creates an account
    Then Vera is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Soren the following permissions:
      | permission               |
      | Can Create/Delete Poll   |
      | Can Add/Delete Poll Pubs |
      | Can Complete/Reschedule  |
    And the poll email members save these preferences:
      | actor | opens | completes |
      | Soren | true  | true      |
      | Tessa | true  | false     |
      | Uma   | false | true      |
      | Vera  | false | false     |
    Given the following journey venues are available:
      | name             | type       |
      | Email First Pub  | pub        |
      | Email Other Pub  | pub        |
      | Email Restaurant | restaurant |
    When Soren creates the notification journey poll
    Then Last Orders submits the journey poll's "poll_opened" personal email only to:
      | recipient |
      | Soren     |
      | Tessa     |
    When Soren adds "Email First Pub" to the notification journey poll
    And Soren manually completes the notification journey poll at "Email First Pub"
    Then Last Orders submits the journey poll's "poll_completed" personal email only to:
      | recipient |
      | Soren     |
      | Uma       |

  Scenario: Changing the main venue submits a reschedule email
    When Soren reschedules the notification journey event to "Email Other Pub"
    Then Last Orders submits the journey poll's "poll_rescheduled" personal email only to:
      | recipient |
      | Soren     |
      | Uma       |
    And Uma sees the notification journey event at "Email Other Pub" without voting
    And the poll email members disable email notifications

  Scenario: Changing only the meal plan submits reschedule emails
    When Soren sets the notification journey meal to "Email Restaurant" at "18:30"
    Then Last Orders submits the journey poll's "poll_rescheduled" personal email only to:
      | recipient |
      | Soren     |
      | Uma       |
    When Soren sets the notification journey meal to "Email Restaurant" at "19:00"
    Then Last Orders submits the journey poll's "poll_rescheduled" personal email only to:
      | recipient |
      | Soren     |
      | Uma       |
    And Uma sees the notification journey event at "Email First Pub" without voting
    And the poll email members disable email notifications