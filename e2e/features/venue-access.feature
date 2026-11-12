@last_orders
Feature: Public venue access
  As an event viewer
  I want venue details to load without venue-management permission
  So that current and past event information remains public

  Scenario: Anonymous visitors can view venue details in current and past events
    Given a fresh emulator database has been bootstrapped with the journey admin
    Given a venue is available to current and past events
    When anonymous views the public venue in Current Events
    Then anonymous sees the public venue details in Current Events
    When anonymous views the public venue in Past Events
    Then anonymous sees the public venue in Past Events

  Scenario: Signed-in non-managers can view events and the read-only venue catalogue
    Given a fresh emulator database has been bootstrapped with the journey admin
    And a venue is available to current and past events
    When Erin creates an account
    Then Erin is logged in
    When Erin views the public venue in Current Events
    Then Erin sees the public venue details in Current Events
    When Erin views the public venue in Past Events
    Then Erin sees the public venue in Past Events
    When Erin views the venue catalogue without management permission
    Then Erin sees the public venue without management actions
