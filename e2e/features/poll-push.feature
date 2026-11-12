@last_orders
Feature: Last Orders submits eligible poll pushes
  Background:
    Given a fresh emulator database has been bootstrapped with the journey admin
    And notification suppression is off in the fresh emulator
    And Nora's browser will grant push permission
    And Owen's browser will grant push permission
    And Pia's browser will grant push permission
    And Quin's browser will grant push permission
    And Rhea's browser will grant push permission
    When Nora creates an account
    Then Nora is logged in
    When Owen creates an account
    Then Owen is logged in
    When Pia creates an account
    Then Pia is logged in
    When Quin creates an account
    Then Quin is logged in
    When Rhea creates an account
    Then Rhea is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Nora the following permissions:
      | permission              |
      | Can Create/Delete Poll  |
      | Can Add/Delete Poll Pubs |
      | Can Complete/Reschedule |
    And the poll push members save these preferences:
      | actor | enabled | opens | completes |
      | Nora  | true    | true  | true      |
      | Owen  | true    | true  | false     |
      | Pia   | true    | false | true      |
      | Quin  | true    | false | false     |
      | Rhea  | false   | true  | true      |
    Given the following journey venues are available:
      | name           | type |
      | Push First Pub | pub  |
      | Push Other Pub | pub  |

  Scenario: Poll creation and manual completion respect independent push preferences
    When Nora creates the notification journey poll
    Then Last Orders submits the journey poll's "poll_opened" push only to:
      | recipient |
      | Nora      |
      | Owen      |
    When Nora adds "Push First Pub" to the notification journey poll
    And Nora manually completes the notification journey poll at "Push First Pub"
    Then Last Orders submits the journey poll's "poll_completed" push only to:
      | recipient |
      | Nora      |
      | Pia       |
    And Pia sees the notification journey event at "Push First Pub" without voting
    And the poll push members disable push notifications
