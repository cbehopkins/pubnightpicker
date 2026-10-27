Feature: Administrators can grant chat access
  As an administrator
  I want to grant chat permission to a newly registered user
  So that they can join the chat without signing in again

  Scenario: Alice gains chat access while she remains signed in
    Given a fresh emulator database has been bootstrapped with the journey admin
    When Alice creates an account
    Then Alice is logged in
    And Alice cannot see the Chat Page
    When the journey admin logs in through a separate browser context
    Then the admin sees Alice on the Manage Users page
    When the admin grants Alice CanChat rights
    Then Alice can open the Chat Page without reloading or signing in again
