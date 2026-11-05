# Staff changes and guards

Authorized users preview and confirm edits to basic employee fields, softphone switches and nonsecret SIP-line settings.

## Sub-features

- `staff-update-plan`: supported fields and a value-redacted offline plan.
- `staff-update-confirm`: noninteractive writes refuse dispatch without confirmation.
- `staff-update-effect`: one PUT, fresh read-back and unchanged unrelated state.

## How to get to it (user POV)

- `uis staff employees update ID --payload-file FILE`.
- `uis staff softphone update ID --payload-file FILE`.
- `uis staff sip-lines update ID --payload-file FILE`.
- `--dry-run` previews; `--force` confirms an independently authorized change.

## Driving it with verify-uis-cli

Preconditions: launch/doctor passed for local guards. A live effect journey additionally requires a specifically authorized disposable employee/line, exact patch, expected prior state and restoration plan; none is supplied by this map.

- **Preview all updates.** Run `python3 "$VERIFY_DRIVER" drive --run-dir "$VERIFY_RUN" --feature staff-guards`. All three real commands must exit 0 for dry-run plans, then exit 10 for unconfirmed writes. The working HTTP observer sees zero requests; credential marker is absent; scratch and Git refs remain unchanged.
- **Preserve proof.** Read `$VERIFY_RUN/evidence/staff-guards.json` plus action/exit records in `commands.jsonl`; run cleanup and verify both survive.
- **Protocol effects.** Run `go test ./internal/portal -run 'TestStaffUpdate' -count=1`. This separately labelled HTTP-boundary coverage proves send-once/read-back logic against local scripted responses, including cancellation and collateral changes. It is not live UIS acceptance.
- **Live effect.** Until the required fixture/patch authorization exists, report `staff-update-effect: unverified (no authorized production fixture)`. Do not substitute guard/protocol results or use a working operator as a test fixture.

## Gotchas

- Dry-run skips authentication, network and persistence; it cannot prove a provider accepts the PUT DTO.
- Exit 9 means reconcile the same identity before any retry. No blind resend or automatic rollback after uncertain dispatch.
- Credential/rights/licence edits, registration reset, SIP reassignment and group CRUD are outside these narrow update commands.
- No ETag/version contract was observed; shared live editing is not isolated.
