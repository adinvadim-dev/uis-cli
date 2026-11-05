# UIS CLI verification map

Read the owning feature before driving it. Launch/doctor/cleanup are in
[verify-uis-cli](../SKILL.md). The helper drives the real compiled CLI; existing
production suites also spawn real CLI processes. No browser UI belongs to this
project: the UIS cabinet is a first-party contract/evidence source.

## Baseline preconditions

Local guards need Go/CGO/Python only and synthetic data. Live reads additionally
need an authorized bound profile, existing session and feature fixtures. Select
`UIS_E2E_CONFIG` and `UIS_E2E_ACCOUNT` explicitly. Never copy secrets into commands,
feature files or evidence. Keep inherited production write/login opt-ins unset.

## Features

| Feature | Entry points / proof |
| --- | --- |
| [Staff diagnostics](staff-diagnostics.md) | Employee and group list/detail, membership, softphone, linked SIP state and registration views; live GET comparison |
| [Staff changes and guards](staff-changes.md) | All three update commands: offline plans, confirmation refusal, provider-effect proof distinguished explicitly |
| [Calls and discovery](calls.md) | Search/detail/legs, reports and filters; existing live production suite |
| [Support archive](support.md) | Sync/local list/show/search and isolated logout; optional support lifecycle requires separate authorization |
| [Autodial](autodial.md) | Supervisor/catalog reads, generated/raw routes and mutation guards; independent lifecycle opt-ins |

## Proof and incomplete coverage

Each recipe has four H2s: sub-features, user entry points, driving instructions and
gotchas. Cover every listed entry point or report it unverified. A fixture error
fails its journey; proving one convenient command does not verify the others.
Local guard evidence cannot certify a live mutation. Never automatically run the
support/campaign write opt-ins or consume a new lifecycle fixture to get a green
summary. Evidence survives cleanup; copied sessions do not.
