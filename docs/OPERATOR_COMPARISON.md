# Compare operator work across UIS and Medivey

Use the two CLIs directly. Collect one explicit period, save each response once,
then reuse those files for each employee. No new Medivey endpoint or grant is
needed. The [offline helper](../scripts/operator_compare.py) has no network,
credentials or writes; it prints aggregates, IDs and coverage without phones.

## 1. Bind the employee once

```sh
uis --account medivey --json staff employees list > staff.json
medivey cc leaderboard --date-from 2026-11-09 --date-till 2026-11-09 --json > leaderboard.json
```

Use the existing authorized employee wrapper for `medivey`, not an admin
or another agent's profile. Staff responses identify UIS employees; leaderboard
rows carry `employeePk`, `employeeName`, `extUserId`. Resolve the same person in
both fresh directories, check an exact unique name and the IDs, and record the
binding. A name match alone is insufficient when names are ambiguous. Do not
assume an Authentik pk equals a UIS employee ID. A leaderboard row may use another
provider's alias; verify each author alias before including it. Missing/inactive
employees or unresolved aliases remain unknown.

Set `UIS_NAME`, `UIS_ID` and `MEDIVEY_EXT_ID` to those verified values. These are
identities, not phone numbers. An employee ID taken from a private report cell is
not a supported numeric *private query* predicate: query the exact cabinet name,
then verify returned `employees[].entity_id` locally.

## 2. Export a bounded period

Use a protected working directory outside repositories (`umask 077`). The files
below can contain customer data. Use a fresh filename/directory for every
observation; do not overwrite the original 18:00 slice with a later read.

```sh
umask 077
uis --account medivey --json --timeout 20s --retries 0 calls search \
  --source private --employee "$UIS_NAME" --analysis --all \
  --from '2026-11-09 00:00:00' --till '2026-11-09 18:00:00' \
  --sort communication_id --order asc --limit 1000 --max-pages 25 > uis-calls.json

medivey lead transfer list \
  --date-from '2026-11-09T00:00:00+03:00' \
  --date-to '2026-11-09T18:00:00+03:00' --all --json > map-transfers.json
```

`--analysis` requests the source-observed `call_type`, talk/total duration and raw
MAV evidence. Live read on 2026-11-10 for the 2026-11-09 interval returned
`call_type.value_id=auto_out_calls` and `is_mass_auto_call_enabled=Нет` on the
same records: **MAV is not the autodial classifier**. Use `call_type.value_id`.
Do not infer autodial from a null scenario or the UIS transfer mark.

`--all` requires explicit private source/from/till, with no date-hint. It reads
at most 100 configured pages, 50,000 rows and 150 seconds total. Default page
cap is 25; inspect `data.coverage.complete`, `reported_total`, `returned`,
`initial_offset` and `next_offset`. Count drift, duplicate IDs and short pages
before the provider count fail instead of claiming completeness. Page/record
caps return an explicit incomplete snapshot with a continuation offset. Starting
from a nonzero offset never establishes complete population coverage. An empty
response without a reported total is unknown, not proven zero. A network/auth
failure returns an error, not a partial zero. No implicit login/refresh.

UIS dates are literal cabinet wall time. Confirm the account timezone before
aligning them with Medivey's offset ISO instants; `--time-zone` below records an
explicit caller assertion, not timezone discovery. Bounds here are inclusive.
Stable counts and offset paging do not provide snapshot isolation. Retain
collection times and hashes; a fresh read is a fresh observation.

## 3. Compare locally by employee

```sh
python3 /path/to/uis-skill/scripts/operator_compare.py \
  --uis uis-calls.json --transfers map-transfers.json \
  --uis-employee-id "$UIS_ID" --medivey-ext-user-id "$MEDIVEY_EXT_ID" \
  --time-zone Europe/Moscow
```

Repeat `--medivey-ext-user-id` only for additional verified aliases of the same
person (for example `authentik:<pk>`). The helper filters UIS participants by
numeric entity ID and map transfers by their own author alias, deduplicates by
each source's identity, checks period boundaries and preserves incomplete or
unresolved coverage. It refuses duplicates rather than silently double counting.
A multi-participant UIS call appears once for the selected employee; do not add
separate employee reports to compute an organisation-wide unique total.

The result gives UIS calls/conversations per source and a separate map-transfer
count/list for the same employee and period. Incoming direction wins; documented
`auto_out_calls|progressive_call` identify UIS autodial; `sip` is only manual-SIP
transport evidence; retailCRM/amoCRM/Bitrix evidence identifies those provider
integrations, not necessarily Medivey CRM. Unrecognised types and absent duration
remain unknown. UIS transfer marks remain separate from map transfers.

The helper separates `pagination_complete` from `employee_period_coverage`.
The UIS export retains its actual `query_scope.filter`: scenario/phone/name or
other filters cannot certify full employee coverage, even with all pages read.
An unfiltered complete UIS snapshot with resolved participants can establish its
employee cohort. Medivey's current wrapper does not echo its query bounds or
filters, so its period coverage remains explicitly unknown; an earlier or narrower
export may contain only rows inside the intended interval. Save the actual
commands with the artifacts and verify the two scopes before interpreting counts.

This is **employee cohort comparison**, not call-to-transfer conversion. The
current Medivey transfer wrapper omits `provider`, `extId`, `leadId`; private UIS
`communication_id` is not established as the classic session ID. Do not join
masked phones, invent exact links from employee/time, or assign a transfer's
source from a coincident key reveal. Input hashes let another run distinguish
changed observations, not recover an unavailable past member set.

## 4. Add context only when needed

```sh
medivey cc key-activity --date-from 2026-11-09 --date-till 2026-11-09 --json
medivey lead transfer get "$TRANSFER_ID" --json
medivey lead journal --phone "$PHONE_OR_MASK" --types transfer,call,crm_lead --all --json
uis --account medivey --json calls show "$COMMUNICATION_ID" \
  --from '2026-11-09 00:00:00' --till '2026-11-09 18:00:00'
```

Keys are a subset of manual dialing, never an incoming line. `key-activity`
counts disclosures and lead-linked transfers; it contains neither UIS call
counts nor proof of dialing intent. Its reveal owner is `operatorUserId`
(Authentik pk); transfer author is `extUserId`. The journal may be incomplete
and CRM phone-linking alone does not establish CRM initiation. Inspect exact
call/transfer facts only when available; label unsupported links as unconfirmed.
Do not fetch every call's details for a simple operator summary.

## Historical comparisons

Keep an original aggregate and a later observation separate. Without the original
member set, the cause and source of their difference remain unproven. Do not use
fresh exports to replace a preserved observation or reconstruct missing members.
The helper intentionally reports no exact per-call map links with the current
wrapper. This is a known data boundary, not a reason to manually label every
transfer or guess missing sources. Keep operational counts and raw live evidence
private; repository examples and fixtures use synthetic data.
