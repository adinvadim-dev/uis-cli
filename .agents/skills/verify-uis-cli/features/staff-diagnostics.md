# Staff diagnostics

Operators inspect a card, membership, softphone switches and SIP registration to compare a failing employee with a working one.

## Sub-features

- `staff-card`: list and exact-ID card with linked SIP records.
- `staff-groups`: group catalog/detail and employee membership.
- `staff-softphone`: three named switch settings.
- `staff-sip`: all lines, exact employee filter, detail and current/last registrations.

## How to get to it (user POV)

- `uis staff employees list`, `get ID`, `groups ID`.
- `uis staff groups list`, `get ID`.
- `uis staff softphone get ID`.
- `uis staff sip-lines list`, `list --employee-id ID`, `get ID`.

## Driving it with verify-uis-cli

Preconditions: launch/doctor passed; explicit `UIS_E2E_CONFIG`/`UIS_E2E_ACCOUNT`; an existing session, group and employees with registered and unregistered SIP lines.

- **Compare through all views.** Run `python3 "$VERIFY_DRIVER" drive --run-dir "$VERIFY_RUN" --feature staff-read --config "$UIS_E2E_CONFIG" --account "$UIS_E2E_ACCOUNT"`. The helper discovers IDs privately, drives every entry point above, checks exact identities and compares linked-line sets in card/filter views.
- **Prove result.** Read `$VERIFY_RUN/evidence/staff-read.json` and `commands.jsonl`. Both registration booleans must have a comparison record, required softphone fields must exist, and original session/config hashes must be unchanged. Each command must exit 0 with private-cabinet provenance. Then execute the skill cleanup and confirm evidence remains.

## Gotchas

- Doctor/auth-status presence is not live authentication proof.
- Detail-card summary fields may be null; use its linked SIP records. Availability, line state and registration are independent.
- If registration changes during comparison, the run fails; do not reinterpret this as a stable expected state.
- Missing registered/unregistered fixtures fails the comparison. Do not mutate an employee to manufacture fixtures.
- Registration rows are a current/last snapshot, not complete history. Raw CLI output remains private/in memory.
