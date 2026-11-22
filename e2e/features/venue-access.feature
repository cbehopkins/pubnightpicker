@last_orders
Feature: Public venue access
  As a meet viewer
  I want venue details to load without venue-management permission
  So that current and past meet information remains public

  Scenario: Anonymous visitors can view open, current and past Meets
    Given a fresh emulator database has been bootstrapped with the journey admin
    Given a venue is available to open, current and past meets
    When anonymous views the public venue in Open Meets
    Then anonymous sees the public venue in Open Meets
    When anonymous views the public venue in Current Meets
    Then anonymous sees the public venue details in Current Meets
    When anonymous views the public venue in Past Meets
    Then anonymous sees the public venue in Past Meets

  Scenario: Signed-in non-managers can view meets and the read-only venue catalogue
    Given a fresh emulator database has been bootstrapped with the journey admin
    And a venue is available to open, current and past meets
    When Erin creates an account
    Then Erin is logged in
    When Erin attempts to create a venue directly through Last Orders
    Then Last Orders rejects Erin's venue creation with forbidden
    When Erin views the public venue in Current Meets
    Then Erin sees the public venue details in Current Meets
    When Erin views the public venue in Past Meets
    Then Erin sees the public venue in Past Meets
    When Erin views the venue catalogue without management permission
    Then Erin sees the public venue without management actions
