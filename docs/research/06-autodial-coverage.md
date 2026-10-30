# Autodial coverage

Inventory 2026-10-30. `campaigns` in the existing Data API is advertising, not autodial. Parameters named campaign_* in call origination do not establish campaign CRUD. The official [outgoing campaign index](https://comagic.github.io/data_api/auto_calls/) publishes four methods; no Call API campaign CRUD contract was established. Call API origination and cabinet `listen_audio_message` initiate billable calls and were not invoked.

Private source: supervisor-workplace frontend build 2026-10-19T12:32:02.617Z, chunk `85455.f0995dee.chunk.js`, AutoCallStore, CampaignModel, CampaignStatusStore, ExcludedNumbersStore. Base is `https://uis-prod-msk-comagic-supervisor-workplace-api.uiscom.ru/v1/common`. This is source evidence, not a network capture or a stability promise. Reads use the bound cabinet JWT. Never infer supervisor access from successful support authentication.

| Operation | Surface | Endpoint / method | CLI | Status |
| --- | --- | --- | --- | --- |
| List, contact outcomes | Data API | get.auto_call_campaigns | auto-call-campaigns list | implemented; key required, not verified live |
| Add numbers | Data API | create.auto_call_campaign_phone_numbers | auto-call-campaign-phone-numbers create | dry-run-only validation; not dispatched live |
| Remove numbers | Data API | delete.auto_call_campaign_phone_numbers | auto-call-campaign-phone-numbers delete | dry-run-only validation; final statuses cannot be deleted |
| Set status, start/resume/pause/stop | Data API | set.auto_call_campaign_status | auto-call-campaign-status set | dry-run-only validation; in_process/planned may call immediately |
| List campaigns | private | POST /auto_call/campaign_list (read) | autodial list --payload-file | verified-live; bounded date window in payload, no completeness claim |
| Get campaign / contacts / settings / schedule | private | GET /auto_call/campaign?campaign_id | autodial get ID | verified-live; settings returned together |
| Allowed status transitions | private | GET /auto_call/campaign_status | autodial statuses | verified-live |
| Call progress/results | private | GET /auto_call/campaign/call_stat | autodial calls ID | verified-live |
| Subscriber progress/results | private | GET /auto_call/campaign/subscriber_stat | autodial subscribers ID | verified-live |
| Contact outcomes export | private | POST /auto_call/get_campaigns_numa_list | autodial export --payload-file | verified-live; JSON output, no file locator following |
| Employee groups | private | GET /auto_call/employees_in_groups | autodial employees | verified-live |
| Stop-list read | private | GET /auto_call/excluded_numa | autodial stop-list | verified-live |
| Stop-list add/remove | private | POST/DELETE /auto_call/excluded_numa?numa | autodial exclude/include --payload-file | dry-run-only; write principal contract unverified |
| Create campaign | private | POST /auto_call/campaign | autodial create --payload-file | dry-run-only; default start semantics and safe cleanup unverified |
| Edit settings / phone_list / attempts / schedule | private | PUT /auto_call/campaign?campaign_id | autodial update ID --payload-file | dry-run-only; saving draft may set new_status=planned |
| Start/resume/pause/stop | private | PATCH /auto_call/campaign?campaign_id | autodial status ID --payload-file | dry-run-only; consult statuses, never guess next_statuses |
| Validate numbers | private | POST /auto_call/campaign/validate_numbers | autodial validate --payload-file | verified-live (empty list); returns numbers, does not persist campaign |
| CSV/XLSX import | private | POST /auto_call/campaign/import_numbers multipart file + old_numbers | autodial import | blocked: multipart parsing contract not verified; no guessed local XLSX conversion |
| Clear contacts | private | Replace phone_list via campaign PUT | autodial update ID --payload-file | dry-run-only; no atomic append/clear endpoint observed |
| Delete/archive campaign | private | Not observed | autodial delete ID | blocked: no deletion/archival endpoint established; cancelled is not deletion |
| IVR/scenarios/media | Data API | get.scenarios, get.media_files | scenarios list; media-files list | existing generated surface; campaign uses playlist/employee settings, not proven scenario_id |
| Audio preview | private | POST /auto_call/campaign/listen_audio_message | autodial listen --payload-file | dry-run-only; creates a real outbound call |
| Templates / per-day schedule / limits | private | No separate endpoint observed | autodial get ID | blocked as separate entities; do not invent endpoints |

Campaign settings observed: number_capacity_usage_rule/id/group_id, pure_ani_phone_list_id, employee_ids, first_call_direction, begin_date/end_date, subscriber_time_from/till, is_use_subscriber_timezone, attempts (subscriber_busy, subscriber_not_responsible, subscriber_number_problems, other), subscriber_dialing_timeout, subscriber_to_employee_ratio, is_predict_enabled, predicted_talk_duration, good_call_min_duration, after_call_employee_busy_max_duration, subscriber_playlist.items (media_file_id or tts_string), funnel/auto/target marks. Full replacement semantics require retaining the other fields. Import returns numbers into the local editor and does not itself save the campaign.

Live access: local auth status had no configured session/helper/key. Existing mini session was found through explicit SSH identity; initial supervisor status, exclusions and employee reads all returned HTTP 401 with exact body `401: Unauthorized`. No mutation or call was sent. Refresh and final probe results are recorded below when available. Provider bodies containing business data are never committed.

Private writes fail closed even with --force until their principal/response and safe lifecycle contracts are established. Their dry-run plans record method/path/parameter names without customer content. Production campaign lifecycle E2E is blocked by the absence of a proven non-starting create plus delete/archive contract. Creating a resource that cannot be cleaned up would violate the test requirement.

Refresh result (mini, 2026-10-30): `auth refresh --surface private --no-input` exited 3 with code `authentication_required`, message `Credentials or private session required`. No repeated login/solver attempts were made. The local production probe is opt-in and was not represented as a successful live run.

## Final live observations

JWT-only candidate CLI requests succeeded on mini: statuses, employees, exclusions, campaign list, get of an existing campaign, call_stat, subscriber_stat, JSON export and empty-number validation. The earlier probe included a Go billing cookie as well as JWT; its 401 is not proof of a missing supervisor entitlement. The candidate sends cabinet JWT only. One month-wide list timed out at 30 seconds; a one-day request with a 60-second bound returned an array. The date filtering semantics and account-wide completeness were not independently established. No returned campaign IDs, phone numbers, employee names or business bodies are published here.

Get returned an object with id, status, phone_list and the settings above, plus max_parallel_calls_count, staff_group_id, message_template, operator_comment, is_operator_auto_answer_enabled and numa_dnd_interval. Call/subscriber stats returned chart_data, grid_data, name, status. Export returned an empty array for the selected campaign/status query: nonempty export columns and completeness remain unverified. Validation of numbers=[] returned numbers=[]. The adapter checks minimum per-operation shapes and requested get identity, not every provider field.

The isolated opt-in `TestProductionAutodial` passed on mini for statuses, employees and exclusions. No campaign mutation, start/resume, audio preview, contact change or spending occurred. Campaign create/delete cleanup and write-principal contracts remain blockers; successful reads do not establish these contracts.
