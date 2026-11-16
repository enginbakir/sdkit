## ADDED Requirements

### Requirement: Apple hidden e-mail

#### Scenario: AC-social-10 - Apple hidden e-mail sign-in
- **THEN** the relay address is stored

## MODIFIED Requirements

### Requirement: Provider errors

#### Scenario: AC-social-2 - Provider error
- **WHEN** the provider returns an error
- **THEN** a retry button is shown with the error message

#### Scenario: AC-social-9 - Last one
- **THEN** nothing else happens

## REMOVED Requirements

### Requirement: Legacy merge

#### Scenario: AC-social-4 - Old merge
- **THEN** gone
