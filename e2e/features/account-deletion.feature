@last_orders @account_deletion
Feature: Account deletion preserves access for other users
  As a user or administrator
  I want an account deleted without affecting other accounts
  So that everyone else can continue using Pub Night Picker

  Scenario: A user deletes their own account
    Given a fresh emulator database has been bootstrapped with the journey admin
    When Alice creates an account
    Then Alice is logged in
    When Bob creates an account
    Then Bob is logged in
    When Alice and Bob have account-deletion votes
    And Alice deletes their own account
    Then Alice cannot log in again
    And Alice's votes are removed and Bob's votes are preserved
    And the journey admin can log in again
    And Bob can log in again

  Scenario: An admin deletes another user's account
    Given a fresh emulator database has been bootstrapped with the journey admin
    When Alice creates an account
    Then Alice is logged in
    When Bob creates an account
    Then Bob is logged in
    When Alice and Bob have account-deletion votes
    And the journey admin logs in through a separate browser context
    And the admin deletes the account for Alice
    Then Alice cannot log in again
    And Alice's votes are removed and Bob's votes are preserved
    And the journey admin can log in again
    And Bob can log in again
