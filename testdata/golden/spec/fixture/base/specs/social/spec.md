# Social

## ADDED Requirements

### Requirement: Social sign-in

SHALL start the provider flow.

#### Scenario: AC-social-1 - Start flow
- **WHEN** the user taps a provider
- **THEN** the provider screen opens

#### Scenario: AC-social-3 - New user
- **WHEN** a new user returns from the provider
- **THEN** sign-up completion opens

### Requirement: Provider errors

#### Scenario: AC-social-2 - Provider error
- **WHEN** the provider returns an error
- **THEN** the error message is shown

#### Scenario: AC-social-9 - Last one
- **THEN** nothing else happens

### Requirement: Legacy merge

#### Scenario: AC-social-4 - Old merge
- **THEN** accounts are merged

#### Scenario: AC-social-5 (withdrawn) - Older merge
- **THEN** WITHDRAWN: replaced by AC-social-4
