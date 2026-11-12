Feature: Admin can open their preferences
  As an administrator
  I want to sign in to Pub Night Picker
  So that I can view my account preferences

  Scenario: Bootstrap an admin and view My Preferences
    Given a fresh emulator database has been bootstrapped with the journey admin
    When I open the Pub Night Picker homepage
    Then I should see the public homepage
    When I log in as the journey admin
    Then I should see My Preferences
