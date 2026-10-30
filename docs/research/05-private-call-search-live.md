# Cabinet call search: live contract

Verified 2026-10-30 from mini, account medivey, with no Data API access key. Read-only report queries and authentication only; two SmartCaptcha login attempts used 2Captcha after explicit authorization. No exports, recognition, support writes, saved-filter/report-state writes, or call control.

## Authentication finding

Classic app Bearer and infopin access_token are insufficient for the new reports. The go cabinet sets a domain `auth` cookie containing JSON `{jwt: ...}`. Shell module 36767 / interceptor in `28188.6ca386fd.chunk.js` sets Authorization Bearer, X-Auth-Token and X-Auth-Type jwt. This JWT authenticates both cabinet RPC metadata and report-constructor. It is a private login credential, not a provisioned Data API key. Saved app/go credentials alone yielded redirects or access_token_invalid. An authenticated support read does not prove report access.

Sources: go.uiscom.ru main.c62caec2.js; shell 28188.6ca386fd.chunk.js; reports library 58323.7050d86b.chunk.js (modules 44568, 64495, 2594, 75281). These are current provider bundles, not an invented route derived from public method names.

## Confirmed read protocols

- POST https://uis-prod-msk-report-constructor-api.uiscom.ru/builder/report, JSON mode=flat, report_type=calls, date_from/date_till naive `YYYY-MM-DD HH:MM:SS`, fields string array, limit integer, offset integer, optional filter object, sort array of `{field, order}`. Omitting fields returns value_error.missing, field=fields. No report_id is necessary for this projection.
- Response data is an array; metadata.total_items is the filtered count on nonempty results. Empty results observed as data=[] and metadata={} (count unknown). Cells can be null, scalar, `{value,value_id}`, or arrays of entity cells. communication_id is a JSON integer; do not coerce to float.
- POST https://dataapi.uiscom.ru/v2.0 with JSON-RPC 2.0 and cabinet JWT headers, no access_token parameter: get.report_filters / get.report_rapid_filters / get.custom_filters with report_type=calls; get.reports_list with params={}. Adding report_type to get.reports_list is rejected as unexpected_parameters. These are cabinet-authenticated calls to the RPC host, distinct from the CLI's key-based Data API transport. List, rapid, saved filters and reports were all successfully verified through the candidate CLI.

## Search matrix

Equality predicates are `{field,operator:"=",value}`; combine with `{condition:"and",filters:[...]}`. The source exposes operators and account-specific values via get.report_filters.

| CLI intent | Cabinet field and value | Live observation |
| --- | --- | --- |
| ID | communication_id, integer | One matching row; metadata total=1 |
| Exact phone | contact_phone_number, string | One matching row; total=1 |
| Direction | call_direction, ["Входящий звонок"] or ["Исходящий звонок"] | Incoming filtered page accepted; enum cells returned |
| Missed | call_status, ["Потерянный"] / ["Принятый"] | Lost subset returned |
| Transfer | call_is_transfer, ["Да"] / ["Нет"] | Transfer subset returned |
| Virtual number | virtual_phone_number, [string] | Filtered subset returned |
| Employee | employees, [cabinet name] | A name taken from a real returned entity produced a nonempty subset; an unused catalog name produced an empty page |
| Scenario | call_scenario, [cabinet name] | Catalog, is_not_null and exact existing-name CLI query confirmed (48 matching rows) |
| AND | condition=and | Direction plus transfer produced a valid empty page with metadata={} |
| Paging | limit=2, offset=0/2 | Two pages accepted, total=28154 for the fixed two-day window |
| Sorting | call_start_date_time desc; communication_id asc | Both exercised through CLI on nonempty pages; ID asc pages checked for order and disjoint identities |

The report catalog contains 51 filter fields, including timing/durations, finish reason, employees, scenario, tags, internal calls, phone/operator/region and comments. Catalog values are account-dependent; do not bake them into fixtures or documentation. The catalog provides employee/scenario display names, not their numeric IDs. Returned employee cells have value, entity_id, entity_type; their presence does not establish an ID-filter wire contract. Private ID flags therefore fail explicitly; use names, or explicit Data API mode for its documented ID predicates.

## Limits and unknowns

This is bounded live report search, with offset pagination and no snapshot guarantee. Period boundaries, timezone interpretation/DST, historical retention, maximum provider limit/date span, account-wide completeness and server stability are not established. CLI dates remain literal cabinet wall time; choose an explicit window. CLI limit 1..10000 is a client bound, not a proven provider maximum. No separate quick-search endpoint was confirmed: phone/ID quick lookup uses the same filtered report. No claim that communication_id equals the classic session ID is inferred from its name.

No raw tokens, cookies, credentials, customer names/phones or call bodies belong in this research file. Private captures remain outside Git with restrictive permissions on mini.

## Candidate CLI verification

The candidate completed native private refresh and stored cabinet_session_present=true while dataapi_env_present=false. With no key configured, explicit private searches passed for ID, phone, outgoing accepted non-transferred calls, an existing employee and an existing scenario. ID/phone each returned one row. Employee and scenario queries returned 1328 and 48 matching records respectively in the fixed two-day window. Pages of two records at offset 0 and 2 used communication_id ascending; empty ID lookup succeeded with an unknown count. All four discovery commands succeeded with private.cabinet_rpc provenance. Captures were checked for predicate consistency; no raw business records are included here.
