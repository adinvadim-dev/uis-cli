# Autodial coverage

Inventory 2026-10-30. `campaigns` in the existing Data API is advertising, not autodial. Parameters named campaign_* in call origination do not establish campaign CRUD. The official [outgoing campaign index](https://comagic.github.io/data_api/auto_calls/) publishes four methods; no Call API campaign CRUD contract was established. Call API origination and cabinet `listen_audio_message` initiate billable calls and were not invoked.

Private source: supervisor-workplace frontend build 2026-10-19T12:32:02.617Z, chunk `85455.f0995dee.chunk.js`, AutoCallStore, CampaignModel, CampaignStatusStore, ExcludedNumbersStore. Base is `https://uis-prod-msk-comagic-supervisor-workplace-api.uiscom.ru/v1/common`. This is source evidence, not a network capture or a stability promise. Reads use the bound cabinet JWT. Never infer supervisor access from successful support authentication.

| Operation | Surface | Endpoint / method | CLI | Status |
| --- | --- | --- | --- | --- |
| List, contact outcomes | Data API | get.auto_call_campaigns | auto-call-campaigns list | verified-live through cabinet JWT; auto/private need no API key |
| Add numbers | Data API | create.auto_call_campaign_phone_numbers | auto-call-campaign-phone-numbers create | enabled via key or cabinet JWT; plans/confirmation tested, no live write |
| Remove numbers | Data API | delete.auto_call_campaign_phone_numbers | auto-call-campaign-phone-numbers delete | enabled via key or cabinet JWT; plans/confirmation tested; final statuses cannot be deleted |
| Set status, start/resume/pause/stop | Data API | set.auto_call_campaign_status | auto-call-campaign-status set | enabled via key or cabinet JWT; plans/confirmation tested; in_process/planned may call immediately |
| List campaigns | private | POST /auto_call/campaign_list (read) | autodial list --payload-file | verified-live; bounded date window in payload, no completeness claim |
| Get campaign / contacts / settings / schedule | private | GET /auto_call/campaign?campaign_id | autodial get ID | verified-live; settings returned together |
| Allowed status transitions | private | GET /auto_call/campaign_status | autodial statuses | verified-live |
| Call progress/results | private | GET /auto_call/campaign/call_stat | autodial calls ID | verified-live |
| Subscriber progress/results | private | GET /auto_call/campaign/subscriber_stat | autodial subscribers ID | verified-live |
| Contact outcomes export | private | POST /auto_call/get_campaigns_numa_list | autodial export --payload-file | verified-live; JSON output, no file locator following |
| Employee groups | private | GET /auto_call/employees_in_groups | autodial employees | verified-live |
| Stop-list read | private | GET /auto_call/excluded_numa | autodial stop-list | verified-live |
| Stop-list add/remove | private | POST/DELETE /auto_call/excluded_numa?numa | autodial exclude/include --payload-file | verified-live POST/DELETE 204 plus read-back, synthetic fixture removed |
| Create campaign | private | POST /auto_call/campaign | autodial create --payload-file | live empty-draft probe: HTTP 500, value_error.exception at __root__; outcome unknown, no identity recovered; CLI remains gated |
| Edit settings / phone_list / attempts / schedule | private | PUT /auto_call/campaign?campaign_id | autodial update ID --payload-file | dry-run-only; saving draft may set new_status=planned |
| Start/resume/pause/stop | private | PATCH /auto_call/campaign?campaign_id | autodial status ID --payload-file | dry-run-only; consult statuses, never guess next_statuses |
| Validate numbers | private | POST /auto_call/campaign/validate_numbers | autodial validate --payload-file | verified-live (empty list and invalid phone rejection); returns numbers, does not persist campaign |
| CSV/XLSX import | private | POST /auto_call/campaign/import_numbers multipart file + old_numbers | autodial import --input-file | verified-live CSV and XLSX parse-only multipart; no campaign persistence |
| Clear contacts | private | Replace phone_list via campaign PUT | autodial update ID --payload-file | dry-run-only; no atomic append/clear endpoint observed |
| Delete/archive campaign | private | Not observed | autodial delete ID | blocked: no deletion/archival endpoint established; cancelled is not deletion |
| IVR/scenarios/media | Data API | get.scenarios, get.media_files | scenarios list; media-files list | existing generated surface; campaign uses playlist/employee settings, not proven scenario_id |
| Audio preview | private | POST /auto_call/campaign/listen_audio_message | autodial listen --payload-file | dry-run-only; creates a real outbound call |
| Templates / per-day schedule / limits | private | No separate endpoint observed | autodial get ID | blocked as separate entities; do not invent endpoints |

Campaign settings observed: number_capacity_usage_rule/id/group_id, pure_ani_phone_list_id, employee_ids, first_call_direction, begin_date/end_date, subscriber_time_from/till, is_use_subscriber_timezone, attempts (subscriber_busy, subscriber_not_responsible, subscriber_number_problems, other), subscriber_dialing_timeout, subscriber_to_employee_ratio, is_predict_enabled, predicted_talk_duration, good_call_min_duration, after_call_employee_busy_max_duration, subscriber_playlist.items (media_file_id or tts_string), funnel/auto/target marks. Full replacement semantics require retaining the other fields. Import returns numbers into the local editor and does not itself save the campaign.

Live access: local auth status had no configured session/helper/key. Existing mini session was found through explicit SSH identity; initial supervisor status, exclusions and employee reads all returned HTTP 401 with exact body `401: Unauthorized`. No mutation or call was sent. Refresh and final probe results are recorded below when available. Provider bodies containing business data are never committed.

Private supervisor writes fail closed even with --force until their principal/response and safe lifecycle contracts are established. Their dry-run plans record method/path/parameter names without customer content. Production campaign lifecycle E2E is blocked by the absence of a proven non-starting create plus delete/archive contract. Creating a resource that cannot be cleaned up would violate the test requirement.

Refresh result (mini, 2026-10-30): `auth refresh --surface private --no-input` exited 3 with code `authentication_required`, message `Credentials or private session required`. No repeated login/solver attempts were made. The local production probe is opt-in and was not represented as a successful live run.

## Final live observations

JWT-only candidate CLI requests succeeded on mini: statuses, employees, exclusions, campaign list, get of an existing campaign, call_stat, subscriber_stat, JSON export and empty-number validation. The earlier probe included a Go billing cookie as well as JWT; its 401 is not proof of a missing supervisor entitlement. The candidate sends cabinet JWT only. One month-wide list timed out at 30 seconds; a one-day request with a 60-second bound returned an array. The date filtering semantics and account-wide completeness were not independently established. No returned campaign IDs, phone numbers, employee names or business bodies are published here.

Get returned an object with id, status, phone_list and the settings above, plus max_parallel_calls_count, staff_group_id, message_template, operator_comment, is_operator_auto_answer_enabled and numa_dnd_interval. Call/subscriber stats returned chart_data, grid_data, name, status. Export returned an empty array for the selected campaign/status query: nonempty export columns and completeness remain unverified. Validation of numbers=[] returned numbers=[]. The adapter checks minimum per-operation shapes and requested get identity, not every provider field.

The isolated opt-in `TestProductionAutodial` passed on mini for statuses, employees and exclusions. No campaign mutation, start/resume, audio preview, contact change or spending occurred. Campaign create/delete cleanup and write-principal contracts remain blockers; successful reads do not establish these contracts.

## Repeatable command-level E2E

`TestProductionAutodial` now exercises every private command plus all four official methods through generated and low-level routes. Private reads dispatch; supervisor writes prove dry-run and forced refusal; unsupported delete proves capability-unavailable. Import parses without saving a campaign. Write checks have independently unusable credentials and sessions, protecting production even if a guard regresses. Official mutations prove dry-run and confirmation refusal, never forced dispatch. Official live listing reports missing Data API credentials explicitly, rather than treating a skipped request as provider success.

Export input follows the cabinet UI in `40784.eb87b0fb.js`: `SUBSCRIBER_STATUSES=["unsuccessfully_processed","not_processed"]`, returned columns `numa`, `campaign_name`, `status_name`. Passing every Data API contact status to this private export was rejected by the provider; the surfaces' status domains must not be conflated. Validation tests use an invalid string and require validation_error, rather than sending a callable number.

Final expanded opt-in run on mini (2026-10-30): PASS, 21 subtests passed and one explicitly skipped (official live list: authentication_required, no Data API credential). All nine supervisor reads passed, including nonempty export rows with the cabinet's three string columns and rejection of a non-callable invalid number. Six private-write plan/force guards, two unavailable contracts and four official dual-route plan/confirmation tests passed. No writes or calls were dispatched. Local make build/check and production-tag compilation also passed; local opt-in execution was skipped by design.

## Keyless outgoing RPC and file import

The previous official live-list SKIP was an implementation gap, not an account-access blocker. Direct JWT-only get.auto_call_campaigns returned HTTP 200 with result.data and result.metadata. Both generated and low-level outgoing commands now accept auto/private/dataapi sources; auto selects the existing bound cabinet session and private uses JWT on the RPC host without an access_token parameter. Four production list variants (generated/raw × auto/private) must succeed with an unusable API-key helper and no UIS_TOKEN. Explicit dataapi keeps the key route. Other private RPC methods are not implied.

The three outgoing mutations have working keyless RPC routing with existing confirmation, binding/authentication preflight and single-dispatch behavior. Their success acknowledgement and ambiguous-error semantics are protocol-tested; they were not sent to production. Successful list access does not prove write permission. Supervisor create/update/status/exclusions/listen plans remain distinct and gated; the RPC route provides add/remove-number and status operations without inventing supervisor replacement semantics.

Import uses the frontend multipart fields file and old_numbers. It returns editor numbers and does not persist a campaign; the 10 MiB bound is a CLI bound, not a proven provider maximum. CSV is covered by the opt-in real-binary suite. XLSX bytes use the same server parsing route; XLSX format handling is not yet verified live. Delete/archive and a safe non-starting create/cleanup lifecycle remain unresolved.

Latest keyless/import suite on mini: 22 PASS, zero SKIP. Ten supervisor read/parse commands passed (including CSV import without saving a campaign), and generated/raw RPC listing with auto/private passed with deliberately unavailable API-key credentials. Six supervisor write guards, unavailable delete and four official method plan/confirmation checks passed. Outgoing RPC mutations remain production-unverified; no campaign/number/status mutation or call was sent.

## Real exclusion mutations and XLSX

The next production run validated a minimal XLSX workbook through the real CLI/provider parser, including exact phone-string preservation. Stop-list POST and DELETE each returned HTTP 204 without JSON. Both were confirmed by fresh GETs; the original exclusion set was restored. The CLI now supports these acknowledgements and read-back verification. An independently opted-in real-binary lifecycle (`UIS_E2E_AUTODIAL_EXCLUSION_WRITE=1`) writes a durable exclusive journal; the final stage was cleanup_verified. Result: 24 PASS, zero SKIP. No campaign/contact/status write or outbound call occurred.

Safe campaign cleanup is still unresolved: current frontend handlers expose cancellation but no delete/archive route; authenticated read-only schema discovery returned 404 for openapi.json/swagger.json. OPTIONS did not provide an Allow contract. The provider help mentions deletion generally, so this is an unresolved discovery gap, not a claim that UIS cannot delete campaigns. Creating a resource that might remain cancelled requires an explicit exception to the original delete/archive requirement before dispatch.

Review found and corrected a recovery-journal race: absent reads cannot certify cleanup after an unresolved add, which may commit later. The harness retains add_unresolved unless the add was acknowledged or observed before compensation. Offline late-commit regression demonstrated failure without this fence and success with it; it runs in the ordinary Go suite without production access.

A second recovery review finding was fixed: cleanup must stop before DELETE if persisting remove_dispatch fails. The offline regression fails when the stage error is ignored and passes when dispatch is fenced. Both journal failures leave explicit recovery state; successful cleanup remains separately verified live.

## Authorized empty campaign probe (2026-10-30)

The owner explicitly allowed one empty test campaign to remain cancelled if delete/archive is unavailable. A single authenticated POST /auto_call/campaign used a unique test marker, status=draft, phone_list=[], employee_ids=[], a future begin_date and disabled audio. No caller capacity was selected; this is an empty-draft probe, not a proven valid full campaign DTO. These empty lists prevent dialing even if the requested draft status is ignored.

Result: HTTP 500 with a root exception envelope (loc=[__root__], type=value_error.exception, generic server-failure category). No ID was returned. Three subsequent supervisor-list reads found no exact test-marker match. This is not successful create/cleanup proof, nor proof that every valid campaign payload fails. HTTP 500 remains outcome_unknown; absence in reads does not exclude a late commit. The protected durable journal retains create_unresolved and the original marker; no second POST was sent and no cleanup_verified stage was invented.

Dependent campaign update/schedule, contact add/remove and status mutations were not dispatched because no owned test identity could be established. Existing business campaigns were not used as write fixtures. No campaign start/resume/audio preview, customer call or spend was initiated. A provider-supported non-starting create contract and reconciliation of the first dispatch are required before another create attempt. The previously verified 24-subtest read/import/exclusion suite does not imply this create probe succeeded.
