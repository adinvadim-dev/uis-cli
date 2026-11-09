---
name: uis-cli
description: "UIS calls, employees, SIP diagnostics, campaigns and support."
metadata: {"openclaw":{"requires":{"bins":["uis","python3"]}}}
---

# UIS

Use `uis --account ACCOUNT --json`; if PATH lacks `uis`, use `$HOME/.local/bin/uis`. In a source checkout, build with `make build` and use `bin/uis`. Credentials come from the configured credential process. Keep session files, credentials and recording URLs private.

## Investigate a call or transfer

For a phone number, failed transfer, or “who hung up?”, follow [the bounded investigation recipe](docs/TRANSFER_INVESTIGATION.md). It includes exact commands and the compact JSON helper, `scripts/call_brief.py`. Run the helper beside this installed skill; it reads CLI JSON from stdin without network or credentials. Keep raw results in private files and give the model the compact projection first.

Start with a narrow, explicit date window and the supplied phone. Search, inspect the relevant call details, then answer from the event timeline. Completion means the selected call's time, participants, transfer result and termination evidence are accounted for; name unknowns explicitly. Match the terminating event to its call leg before attributing a hangup. A generic “employee disconnected” alone does not identify the receiving party.

Use the existing bound session first. `account doctor` / `auth status` check presence, not live report access. Refresh once only for an explicit authentication error or missing cabinet session: `auth refresh --surface private --no-input`. A provider/transport error follows the recipe's bounded read recovery, not login. After the read budget is exhausted, return partial findings and the blocker.

## Other operations

Read [README.md](README.md) for commands and [capability limits](docs/IMPLEMENTATION.md) when a requested operation is unavailable. Private search/discovery uses cabinet JWT without a Data API key; exact employee/scenario names come from the private catalog. Use `scripts/call_brief.py fields` to discover field descriptors from the saved catalog, then `filters --field FIELD` for its operators and `--values` when its values are needed. Numeric employee/scenario IDs and `calls legs` require a separate Data API key. Preserve source and ID provenance: cabinet communication IDs are not assumed to equal classic session IDs.

Support: `support status` checks local archive freshness; `support sync` refreshes it; `support ticket list|show|search` reads offline, and `--live` reads the provider. See [authentication](docs/AUTHENTICATION.md) for session binding and captcha requirements.

Writes require explicit task authorization. Inspect `--dry-run`, send once, and verify with a fresh read. Exit 9 means outcome unknown: retain returned identity and reconcile before retrying. Preserve the account binding and preflight checks; invoking this skill grants no mutation authority. SIP/project creation follows the Medivey process skill.

## Autodial

Scheduled conversion reports: use the [bounded reporting helper](docs/AUTODIAL_REPORTING.md) for Moscow-day collection, shared deadlines, empty-state handling and proof without sending. Keep provider delivery receipts separate from helper output.

Use [the coverage table](docs/research/06-autodial-coverage.md) before campaign work. Outgoing campaigns use `autodial` (private supervisor) or generated `auto-call-*` (Data API); `campaigns` means advertising. `autodial statuses` returns the status vocabulary and transitions; `autodial list` returns campaigns. Start with the vocabulary, then `get ID`, `calls ID` and `subscribers ID`. Historical summary mode selects current `in_process` campaigns within its list-date filter; past campaign status requires independent dated evidence. Keep raw contacts/reports in protected files. Use narrow list dates through `--payload-file`; exports are JSON data, with no completeness guarantee.

Create requires future account-local begin_date, nonempty employee_ids/phone_list, and an available caller ID. Resolve references with autodial employees/caller-rules/caller-numbers/caller-groups/caller-lists/timezone; resolve playlist files with media. Creation schedules a planned campaign; it does not save an empty draft. Planned/running/paused updates accept only contacts/employees. Use clear for logical contact removal, cancel for terminal cleanup; successful contact history and the campaign itself may remain. Physical delete/archive is unavailable.

Authorized mutations use confirmation/preflight, one dispatch and read-back verification. On exit 9 preserve the payload, name marker and campaign_id, reconcile current state, and stop automatic retries. Read success is not write proof. Official auto-call-* methods accept --source private with cabinet JWT and no API key; auto prefers a bound session. Use dataapi for the separate key route. Import parses CSV/XLSX without saving; keep returned rows private. Stop-list exclude/include also verify their effects.

Start/resume/in_process, planned transitions and audio preview may initiate calls/charges. Keep activation as dry-run in a no-call test task. Audio preview dispatch is gated. Use synthetic contacts and a far-future start for an explicitly authorized lifecycle; cancellation residue requires explicit permission. Record dispatch/recovery stages durably, never create another campaign after an unresolved attempt. On 401 refresh once; if it fails, report the sanitized blocker and continue independent work.

## Employees, softphone and SIP registration

Use `staff` for the richer **private cabinet** surface. Start with
`staff employees list`, then `staff employees get ID`, `staff employees groups ID`,
`staff softphone get ID`, and `staff sip-lines list --employee-id ID`.
`staff groups list|get ID` resolves the current cabinet group catalog.
For comparisons read both employee IDs freshly. Employee availability,
SIP `is_registered` and line `state` are independent; do not infer registration
from an employee's available/DND status. `registrations` contains only the current
or last rows exposed by the cabinet, not complete history. Outputs omit passwords,
SIP addresses/logins, server/IP details, tokens and contact addresses.

For authorized changes use `staff employees update ID`, `staff softphone update ID`
or `staff sip-lines update ID` with `--payload-file FILE`. Read
[the patch contract and gaps](docs/research/10-employees-coverage.md) and
[CLI_SPEC.md](docs/CLI_SPEC.md) first. Employee edits cover basic fields, schedule
and group membership. Softphone accepts only `is_need_hide_numbers`,
`is_click_to_call_auto_answer_enabled`, `is_out_calls_auto_answer_enabled` booleans.
SIP edits cover `state` (`active`/`manual_lock`), `channels_count` (1–2), and
`is_multiterminal`. Inspect `--dry-run` before supplying `--force` for an explicitly
authorized change. Dry-run does no session/auth/network I/O and prints field names
only. One dispatch plus fresh effect read-back produces `verified=true`; exit 9
requires re-reading the same ID before any retry. Do not print raw cabinet DTOs.

Registration reset is not covered: the editor's password refresh icon rotates a
credential; it does not prove a reset API. Do not rotate credentials to fix
registration. SIP reassignment is a separate compound action and remains deferred,
as do role/license changes, group CRUD and provisioning. This skill grants no
permission to change production employees or access rights.

## Verification

Before delivering CLI changes, use [verify-uis-cli](.agents/skills/verify-uis-cli/SKILL.md) and its maintained feature map. Keep local guard proof, live reads and independently authorized live writes distinct.

## Operator call sources and Medivey comparison

For operator priorities, autodial/manual/incoming counts, keys or cross-system
operator comparison, use [the two-CLI recipe](docs/OPERATOR_COMPARISON.md).
`calls search --source private --analysis --all` exports bounded source/duration
facts; the linked offline helper compares verified employee IDs with existing
`medivey-cli` snapshots. Check coverage and timezone, keep observations separate,
and distinguish employee cohorts from exact call/transfer links. Keys are a
manual subset; MAV, masked phones and shared names do not establish origin.
