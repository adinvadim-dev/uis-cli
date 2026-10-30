---
name: uis-cli
description: "UIS calls, transfers, filters, and support tickets."
metadata: {"openclaw":{"requires":{"bins":["uis","python3"]}}}
---

# UIS

Use `uis --account ACCOUNT --json`; if PATH lacks `uis`, use `$HOME/.local/bin/uis`. In a source checkout, build with `make build` and use `bin/uis`. Credentials come from the configured credential process. Keep session files, credentials and recording URLs private.

## Investigate a call or transfer

For a phone number, failed transfer, or “who hung up?”, follow [the bounded investigation recipe](docs/TRANSFER_INVESTIGATION.md). It includes exact commands and the compact JSON helper, `scripts/call_brief.py`. Run the helper beside this installed skill; it reads CLI JSON from stdin without network or credentials. Keep raw results in private files and give the model the compact projection first.

Start with a narrow, explicit date window and the supplied phone. Search, inspect the relevant call details, then answer from the event timeline. Completion means the selected call's time, participants, transfer result and termination evidence are accounted for; name unknowns explicitly. Match the terminating event to its call leg before attributing a hangup. A generic “employee disconnected” alone does not identify the receiving party.

Use the existing bound session first. `account doctor` / `auth status` check presence, not live report access. Refresh once only for an explicit authentication error or missing cabinet session: `auth refresh --surface private --no-input`. A provider/transport error follows the recipe's bounded read recovery, not login. After the read budget is exhausted, return partial findings and the blocker.

## Other operations

Read [README.md](README.md) for commands and [capability limits](docs/IMPLEMENTATION.md) when a requested operation is unavailable. Private search/discovery uses cabinet JWT without a Data API key; exact employee/scenario names come from the private catalog. Use `scripts/call_brief.py filters --field FIELD` to inspect only the required field's operators, then `--values` when its values are needed. Numeric employee/scenario IDs and `calls legs` require a separate Data API key. Preserve source and ID provenance: cabinet communication IDs are not assumed to equal classic session IDs.

Support: `support status` checks local archive freshness; `support sync` refreshes it; `support ticket list|show|search` reads offline, and `--live` reads the provider. See [authentication](docs/AUTHENTICATION.md) for session binding and captcha requirements.

Writes require explicit task authorization. Inspect `--dry-run`, send once, and verify with a fresh read. Exit 9 means outcome unknown: retain returned identity and reconcile before retrying. Preserve the account binding and preflight checks; invoking this skill grants no mutation authority. SIP/project creation follows the Medivey process skill.

## Autodial

Use [the coverage table](docs/research/06-autodial-coverage.md) before campaign work. Outgoing campaigns use `autodial` (private supervisor) or generated `auto-call-*` (Data API); `campaigns` means advertising. Start with `autodial statuses`, then `get ID`, `calls ID` and `subscribers ID`. Keep raw contacts/reports in protected files. Use narrow list dates through `--payload-file`; exports are JSON data, with no completeness guarantee.

Private supervisor writes are dry-run-only until draft-start, principal and cleanup contracts are established. The four `auto-call-*` methods also support `--source private` with cabinet JWT and no API key; auto prefers an existing bound session. Their mutations are enabled with confirmation/preflight, sent once, and not proven live by read access. Use `--source dataapi` only for the separate API-key path. `autodial import --input-file PATH` parses a file without saving a campaign; keep returned numbers private. `--force` does not bypass that gate. Generated Data API writes still require explicit task authorization and confirmation. Setting `planned` / `in_process`, resuming, or listening to an audio preview may initiate real calls and charges. For a test task, retain these as plans; never put real contacts into a test campaign. On 401 refresh once; if it fails, report the exact sanitized auth blocker and continue independent local work.
