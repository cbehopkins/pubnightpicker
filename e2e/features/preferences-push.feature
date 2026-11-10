Feature: Members voluntarily opt in to browser push
  Background:
    Given a fresh emulator database has been bootstrapped with the journey admin

  Scenario: A member enables push and saves notification categories
    Given Penny's browser will grant push permission
    When Penny creates an account
    Then Penny is logged in
    And Penny has not requested permission or subscribed to push
    When Penny visits My Preferences
    Then Penny starts with push disabled and no endpoint
    And Penny has not requested permission or subscribed to push
    When Penny clicks Enable Push
    Then Penny has an active synthetic push endpoint and enabled preference
    When Penny saves these push notification categories:
      | category                                       | enabled |
      | A poll opens                                   | true    |
      | A poll completes                               | false   |
      | A message is sent in global chat                | true    |
      | A message is sent in an event chat I am attending | false |
    And Penny reloads My Preferences
    Then Penny sees the saved push notification categories
    And Penny has an active synthetic push endpoint and enabled preference
    And Penny has made exactly one voluntary push subscription

  Scenario: Denying push permission leaves no endpoint or enabled preference
    Given Nora's browser will deny push permission
    When Nora creates an account
    Then Nora is logged in
    When Nora visits My Preferences
    Then Nora starts with push disabled and no endpoint
    When Nora clicks Enable Push
    Then Nora sees push permission denied without a subscription
    When Nora reloads My Preferences
    Then Nora still has no push endpoint or enabled preference

  Scenario: Disabling push removes the browser endpoint's eligibility
    Given Dina's browser will grant push permission
    When Dina creates an account
    Then Dina is logged in
    When Dina visits My Preferences
    And Dina clicks Enable Push
    Then Dina has an active synthetic push endpoint and enabled preference
    When Dina clicks Disable Push
    Then Dina has an inactive push endpoint and disabled preference
    When Dina reloads My Preferences
    Then Dina has an inactive push endpoint and disabled preference
