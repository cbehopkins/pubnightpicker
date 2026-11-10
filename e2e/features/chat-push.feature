@last_orders
Feature: Last Orders submits eligible chat pushes
  Background:
    Given a fresh emulator database has been bootstrapped with the journey admin
    And notification suppression is off in the fresh emulator

  Scenario: Global chat push excludes the author and opted-out users
    Given Asher's browser will grant push permission
    And Briar's browser will grant push permission
    And Cato's browser will grant push permission
    And Dove's browser will grant push permission
    When Asher creates an account
    Then Asher is logged in
    When Briar creates an account
    Then Briar is logged in
    When Cato creates an account
    Then Cato is logged in
    When Dove creates an account
    Then Dove is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Asher the following permissions:
      | permission             |
      | Can Chat               |
      | Can Create/Delete Poll |
    And the admin grants Briar the following permissions:
      | permission |
      | Can Chat   |
    And the admin grants Cato the following permissions:
      | permission |
      | Can Chat   |
    And the admin grants Dove the following permissions:
      | permission |
      | Can Chat   |
    When Asher enables push and saves global chat notifications as "true"
    And Briar enables push and saves global chat notifications as "true"
    And Cato enables push and saves global chat notifications as "false"
    And Dove enables push and saves global chat notifications as "false"
    Then Last Orders observes these push profiles:
      | actor | global | event |
      | Asher | true   | false |
      | Briar | true   | false |
      | Cato  | false  | false |
      | Dove  | false  | false |
    When Asher opens Chat Page
    And Asher sends a global chat message
    Then Last Orders processes Asher's global chat push for:
      | recipient |
      | Briar     |

  Scenario: Event chat push excludes the author, opted-out and muted users
    Given Eli's browser will grant push permission
    And Fay's browser will grant push permission
    And Gus's browser will grant push permission
    And Hal's browser will grant push permission
    When Eli creates an account
    Then Eli is logged in
    When Fay creates an account
    Then Fay is logged in
    When Gus creates an account
    Then Gus is logged in
    When Hal creates an account
    Then Hal is logged in
    When the journey admin logs in through a separate browser context
    And the admin grants Eli the following permissions:
      | permission             |
      | Can Chat               |
      | Can Create/Delete Poll |
    And the admin grants Fay the following permissions:
      | permission |
      | Can Chat   |
    And the admin grants Gus the following permissions:
      | permission |
      | Can Chat   |
    And the admin grants Hal the following permissions:
      | permission |
      | Can Chat   |
    When Eli enables push and saves event chat notifications as "true"
    And Fay enables push and saves event chat notifications as "true"
    And Gus enables push and saves event chat notifications as "false"
    And Hal enables push and saves event chat notifications as "true"
    Then Last Orders observes these push profiles:
      | actor | global | event |
      | Eli   | false  | true  |
      | Fay   | false  | true  |
      | Gus   | false  | false |
      | Hal   | false  | true  |
    When Eli creates a poll through the UI for event chat
    And Hal joins the event chat conversation
    And Gus joins the event chat conversation
    And Fay joins the event chat conversation
    And Hal mutes notifications for the journey event
    Then Last Orders observes Hal's event-chat mute
    When Eli sends the final event chat message
    Then Last Orders processes Eli's event chat push for:
      | recipient |
      | Fay       |
