# UIS / CoMagic: official API surface

Research date: 2026-10-29. Scope: public documentation/source and read-only local code inspection; no authenticated account requests, support submissions, or other live mutations. This report distinguishes **documented**, **implemented in Medivey**, **inference**, and **unknown**. Documentation does not establish that a particular UIS account is entitled to a method.

## Main finding

Do not infer the provider's complete surface from the CLI's 148-method snapshot. In particular, provider-owned documentation contains `get.call_details`, `get.calls`, saved/rapid filters, report state, asynchronous exports, and a narrowly described `create.tech_support_ticket`; none appears in the embedded snapshot. These are Data API documentation, not discoveries of cabinet HTTP endpoints. Their UIS-host availability needs a separate, authorized read-only check. [CLI snapshot][spec] [Provider source tree][tree] [Details contract][details] [Support contract][ticket]

There is a significant publication discrepancy: public GETs returned 200 for `comagic.github.io/data_api/report_new/flat/get_calls/`, `.../get_call_details/`, and `.../customer/create_tech_support_ticket/`; the corresponding paths beneath `www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/` returned HTTP 404. This is **documentation-host evidence only**, not a JSON-RPC availability test. The provider GitHub Pages repository was inspected at commit `e457deb1e828bafc2243390f421e5b9db9859737`; links to less discoverable contracts below pin that revision. [Provider repository][provider] [Pinned tree][tree]

## Surfaces, versions, authentication, and promises

| Surface | Contract and appropriate use | Authentication / confidence |
| --- | --- | --- |
| Public Data API | `https://dataapi.uiscom.ru/v2.0`; reports and configuration. CoMagic uses `dataapi.comagic.ru`. Portal credentials are not interchangeable across these two hosts. | User-scoped access key or session; documented. [General rules][general] |
| Public Call API | `https://callapi.uiscom.ru/v4.0`; active calls and call control. Medivey independently uses this version. | User needs Call API access and enabled components; key or login session; documented. [Call API][call-general] [Medivey adapter][m-call] |
| Private legacy cabinet | `app.uiscom.ru`, notably `GET /analytics/call/call_details_data/?call_id=…`. | Cabinet bearer session, not assumed interchangeable with a Data API key; implemented in Medivey. [Private adapter][m-private] |
| Private newer cabinet / billing | `go.uiscom.ru`; Medivey names backend `uis-prod-msk-comagic-api.uiscom.ru`, `billing/total-sdr` and `billing/service`. | Separate SSO client/session; app token rejected according to adapter comments. Observed application contract, not public API guarantee. [Private adapter][m-private] |
| Separate documented Billing API | Provider repository also documents `billing-api.uiscom.ru/v1.0` / `billing-api.comagic.ru/v1.0`. | User access keys; not the above private billing backend. Entitlement and relevance to this account unknown. [Billing docs source][billing] |

Data API rules: HTTPS POST, JSON-RPC 2.0, UTF-8 JSON; `access_token` in parameters; IP allowlist required. User keys inherit cabinet permissions; permanent keys do not expire, temporary keys expire on a date; login sessions last one hour. Current version is 2.0; documentation promises at most two supported versions and two months' deprecated-version support, signaled in metadata. Limits are weighted successful requests, currently one point each, with day/minute remaining/reset metadata. Expansion is configured in the cabinet. [General rules][general]

Call API uses JSON-RPC without batches. Login sessions default to one hour; persistent keys are user-configured. Components separate basic, call-management, informer, and scenario functionality. Quotas attach to the basic component and successful operations consume weighted points. Call control is stateful: obtaining a session ID does not mean a conversation connected. The docs describe the same two-version/two-month compatibility policy; they spell the deprecation flag `current_version_depricated`, unlike Data API's spelling. [Call API][call-general]

**Unknown guarantees:** no API-specific availability SLA, maximum reporting lag, stable snapshot pagination, immutable history, exact recording retention/download lifetime, general idempotency guarantee, or guaranteed support response time was established from these contracts. Do not promote documentation samples into such guarantees. [General rules][general] [Call API][call-general] [Export tasks][export-list] [Support page][support]

## Support / communication with UIS

The official support page offers a problem-submission form and remote help, and asks for identifying context such as organization, email, contract, and virtual number. This establishes a human support channel, not a supported ticket-list/reply API. No form was submitted. [Support page][support]

An important **documented but narrow** mutation exists in provider source: `create.tech_support_ticket`. Its title and description are specifically **connecting a third-party provider's number**, although `context` is described as ticket context. Required table fields: `access_token`, `context`, `contact_id`, `phone_id`, `email_id`; optional `user_id` becomes required for an agent. Response is `success: true`, without a documented ticket ID. The JSON example omits `email_id` and duplicates `user_id`: the table/example disagree. Treat general technical-support submission through this method as **unverified**, not established. [Ticket contract][ticket]

`get.customer_contact_info` documents contact IDs and associated email/phone IDs needed by that action. Its headings are also inconsistent with its response contents. Neither contract supplies ticket history, status, replies, attachments, thread IDs, or reconciliation after a submission timeout. No corresponding ticket read/reply method was found in the inspected provider tree. This is a bounded negative finding, not proof that no private support system exists. [Contact-info contract][contact-info] [Ticket contract][ticket] [Source tree][tree]

For API incidents, Call API error guidance asks for the triggering request and request time when contacting support. **Recommendation:** prepare a sanitized incident bundle locally, stripping tokens and personal/customer data; sending it is a distinct action. A generic `get.chats_report` means the customer's communications, not the customer's conversation with UIS support. [Call API][call-general] [Chat report in CLI snapshot][spec]

## Call detailing: three complementary paths

| Path | Established details | Boundary |
| --- | --- | --- |
| `get.calls_report` | Session row, outcome, durations, participants, recording references, scenario/marketing attribution. | Summary record; do not call it a full event timeline. [Session report][calls] |
| `get.call_legs_report` | Individual CDRs: session join, leg times, calling/called numbers, employee/group, transfer/failure/talk flags, release-cause code/description. | Filter by `call_session_id`; leg `id` is a different identity. [Leg report][legs] |
| `get.call_details` | Provider-documented event detail, including nested `call_events`, timing, event mnemonics/text, failure hints, scenario actions, recording references and comment. | Requires report context and explicit fields; not in CLI snapshot. UIS deployment/entitlement unverified. [Details contract][details] |
| Cabinet `call_details_data` | Medivey models `header` plus nested `grid`, including `fail_hint`, `elk_event_text`, `link_call_session_id`. | Private bearer API; no provider compatibility promise established. [Private adapter][m-private] |

`get.call_details` requires `report_id`, `entity_ids`, `date_from`, and `date_till`, alongside auth; interval maximum is documented as 366 days. `fields` is nominally optional, but the text explicitly says an omitted/empty list yields an empty response. Fields come from report column discovery. Filter depth is at most two. The request example omits required `entity_ids`; table and example conflict. Response examples contain nested objects where some table types are simpler, so a parser must preserve unknown structure rather than assume flat strings. [Details contract][details]

Medivey fetches cabinet details with bearer authorization and a 20-second timeout, validates `data.header` and array `data.grid`, and refreshes private sessions through its wrapper. Its Data API fallback looks up `get.calls_report` with `filter: {field: "id", operator: "=", value: <session-id>}` in a bounded date interval. It synthesizes a partial start/talk/finish timeline and marks `private_details_unavailable: true`; that is **not equivalent** to original provider routing events. [Private adapter, fetch and fallback][m-private]

Identity matters: Medivey explicitly records that `get.calls_report.id` is the session ID referenced by `get.call_legs_report.call_session_id`; the old session report neither returns nor accepts a separate `call_session_id` field. The reference feature doc also describes phone/start-time correlation and the need to preserve identifier provenance. [Session report][calls] [Medivey identity lookup][m-private] [Voice behavior doc][m-voice]

## Quick call search and filters

### Legacy session/CDR reports

`get.calls_report` needs date bounds, supports `filter`, `fields`, `sort`, `limit`, `offset`, and `include_ongoing_calls`. Useful filterable fields include `id`, `contact_phone_number`, `virtual_phone_number`, `direction`, `is_lost`, `finish_reason`, `is_transfer`, durations, `scenario_id`, `campaign_id`, `tags`, `employees`, `call_api_external_id`, `call_api_request_id`, `ext_id`, and attribution fields. `start_time` is sortable but not marked filterable; use date bounds. `search_query` means an advertising/search-engine query, not general call search. [Session report][calls]

**Recommended search composition (inference):** exact session ID plus a known date window is the quickest deterministic old-API lookup. Otherwise use a bounded phone/time search, then examine matching session summaries and legs. Apply server-side direction/loss/scenario filters before fetching extra fields. Medivey already implements phone/time matching and ID/CDR joining. [Medivey public adapter][m-uis] [Medivey identity lookup][m-private] [Voice behavior doc][m-voice]

General filter grammar is `{field, operator, value}` or `{filters: […], condition: "and"|"or"}`. Documented operators include equality/comparison, `like` with `%`, `ilike`, negative variants, `regexp`, `in`/`not_in`, `jsquery` for arrays/objects, overlap and null tests; several are method-dependent. Selecting a returned field does not automatically make it filterable/sortable. Global defaults are limit 1,000, maximum 10,000, offset maximum 100,000; report intervals can trigger a three-month error. Split large searches by time and honor method-specific limits. [Provider general/filter docs][general-mirror]

`get.call_legs_report` additionally supports filters on `call_session_id`, calling/called/employee numbers, `employee_id`, `group_id`, `scenario_id`, `is_failed`, `is_talked`, `is_transfered` (provider spelling), `release_cause_code`, and `release_cause_description`. Do not assume its returned `contact_phone_number` is filterable: the table does not mark it so. [Leg report][legs]

### Report-aware search, saved filters, and discovery

`get.calls` is a separate documented flat-report method: `report_id`, date bounds (366-day limit), explicit column `fields`, filters (two-level maximum), sorting and paging. Empty `fields` means empty response. Rows include `communication_id`, `cdrs`, display-valued status/direction fields, participants, comments, recording data and attribution. It is absent from the embedded CLI snapshot; do not equate it with `get.calls_report`. [Flat calls contract][flat-calls]

| Capability | Documented method / semantics | Existing CLI snapshot |
| --- | --- | --- |
| Discover reports | `get.reports_list`: user report IDs/types, names, favorites and metadata. | Yes. [Reports list][reports] [Snapshot][spec] |
| Discover ordinary filters | `get.report_filters(report_type)`: IDs, types, operators, candidate values. Values may be only top 10 from the last seven days, not a complete value dictionary. | Yes. [Filter discovery][filters] [Snapshot][spec] |
| Discover rapid filters | `get.report_rapid_filters(report_type)`: one operator object per condition; candidate values likewise potentially top 10/seven days. | No. [Rapid-filter contract][rapid] [Snapshot][spec] |
| Read saved filters | `get.custom_filters(report_type)`: IDs, names, stored filter trees, paging. | No. [Saved-filter contract][saved] [Snapshot][spec] |
| Create/delete saved filters | `create.custom_filter(report_type,name,filter)`; `delete.custom_filter(id)`. These mutate account state. Create table calls `report_type` a number while read uses enum: resolve before implementation. | No. [Create][saved-create] [Delete][saved-delete] [Snapshot][spec] |
| Read/write report state | `getobj.report_state(report_id)` / `set.report_state(report_id,state)`: date ranges, dimensions, columns, `filter`, `rapid_filter`, sort, widths; setter also describes `columns_filter`. | No. [Read state][state] [Write state][state-set] [Snapshot][spec] |
| Discover report columns/dimensions | `getobj.columns_tree`, `getobj.dimensions_tree`, both parameterized by `report_type`. | Yes. [Snapshot][spec] |
| Aggregate/filter data | `getobj.report_table`: `call_summary_uis` or `summary_analytics`, dimensions/columns, ordinary and column filters, sort, branch expansion. Limit max 200/default 50; 366-day range; filter depth two. | Yes. [Table contract][table] [Snapshot][spec] |
| Aggregate totals | `getobj.report_total`: its own page limits support to `call_summary_uis`; columns and filters. Do not inherit table's broader report-type support. | Yes. [Totals contract][totals] [Snapshot][spec] |

The cabinet documentation separately describes saved filters, per-column quick filters and context-menu filtering by caller/employee, plus the call-routing detail view. Thus UI parity is broader than an ad hoc report predicate. **Inference:** prefer the documented discovery/state/filter methods above before building private equivalents, but first verify host availability without changing report state. [Calls UI guide][calls-ui] [Report-state contracts][state] [Saved-filter contract][saved]

## Reads versus actions and asynchronous operations

Data API mutations already represented in the snapshot include communication comments, communication tags, contact/employee/SIP/configuration CRUD and `upload.calls`. These are not read-only report operations. Newly found saved-filter/report-state setters and support submissions are additional mutations. [Snapshot][spec] [Saved-filter create][saved-create] [State setter][state-set] [Ticket contract][ticket]

Call API includes originate methods such as `start.employee_call`, returning `call_session_id`; it starts a call rather than proving connection. `list.calls` is the active-call read, with direction and virtual-number filters and leg state. It is not historical search. `record.call` controls active recording but cannot disable globally enabled recording. `transfer.talk` requires prior `make.call` and valid call state; methods are not freely interchangeable. [Originate][originate] [Active calls][active] [Recording control][record] [Transfer][transfer]

Provider-documented asynchronous surfaces absent from the snapshot:

| Operation | Contract | Unresolved |
| --- | --- | --- |
| Create report export | `create.report_generator_task`: report type/name, date bounds, dimensions, columns; optional `csv`, `xlsx`, `google_sheets`; filter, column filter, sort; returns task `id`. | Completion deadline, duplicate suppression, cancellation, and Google Sheets authorization/effects unspecified here. [Create export][export-create] |
| Poll export | `get.report_generator_tasks`: filter by `id`/`state`, default 50/max 200; returns `task_id`, `state`, `file_name`, format. States: `new`, `in_process`, `file_ready`, `processed`, `error`, `file_expired`. | Download URL/auth/lifetime not established by these fields. `file_name` alone is not a download contract. [List exports][export-list] |
| Request transcription | `create.call_session_recognition_task(call_session_id)`. | Charging, duplicate task semantics and completion SLA unknown. Do not trigger just to inspect. [Recognition task][recognize] |
| Read/search transcription | `getobj.call_session_recognition_info(call_session_id, search_text?)`: phrase list, timing, operator marker, edited phrase, engine and recording information. | `search_text` is per-session phrase search, not documented global full-text call search. [Recognition info][recognition] |

**Recommendation, not provider guarantee:** execute mutations once; reconcile ambiguous outcomes only through established reads. A JSON-RPC request `id` correlates a request/response; no reviewed contract makes it a deduplication key. Support submission currently lacks a documented read that could safely resolve a lost response. [General rules][general-mirror] [Ticket contract][ticket] [Export contracts][export-list]

## Pricing and quota uncertainty

The current Extended API product page advertises **1,590 RUB/month**. Its displayed quotas: Data API 500/day and 60/minute, with extra request prices 0.6 RUB and 15 RUB respectively; Call API 500/day and 15/minute, with extra request prices 1 RUB and 10 RUB. These are published offer figures, not observed account configuration. [Extended API product page][extended]

The provider's **archived 2026-02-20** tariff PDF instead lists Basic API at **890 RUB/month**, Data API 500 points/day and 60/minute, but the daily extra point price is **0.06 RUB**. This conflicts by 10× with the current product page's daily Data API extra-request price. Do not silently choose one, or claim that all existing accounts pay the advertised price. The Basic API product page did not yield a current numeric price in the text inspected. Contract-specific price, included entitlements, tax treatment and overage activation remain unknown. [Archived tariff PDF][tariff] [Basic API page][basic] [Extended API page][extended]

No suitable universally free API entitlement was established. Reuse existing authorized API access when entitled; neither new subscriptions nor quota expansion is authorized by this research. [API product descriptions][basic] [Extended API page][extended]

## Existing uis-cli coverage and limits

Inspected CLI commit: `5d8abd4905e341d735ad5dd208900e41c4ef17a9`. The JSON snapshot contains **148 methods**, all without populated `api_version`. Its schema records method/title/docs URL/description and a flat parameter list, not response schemas or a complete typed contract. `make spec` runs the repository's own HTML scraper, then `make gen` generates resource commands: this is **not a provider-issued OpenAPI document**. [Snapshot][spec] [Loader][loader] [Scraper][scraper] [Makefile][make]

| Included relevant method(s) | Existing parameters/capability |
| --- | --- |
| `get.calls_report` | Required dates; `fields`, `filter`, `include_ongoing_calls`, `limit`, `offset`, `sort`, `user_id`. |
| `get.call_legs_report`, `get.financial_call_legs_report` | Required dates; fields/filter/sort/paging; raw legs and financial legs are separate reports. |
| `get.communications_report` | Cross-channel report with dates, fields/filter/sort/paging. |
| `get.chats_report`, `get.chat_messages_report`, `get.offline_messages_report`, `get.goals_report` | Adjacent communication reports; not UIS support history. |
| `get.ct_summary_report`, `get.ct_ac_summary_report`, `get.visitor_sessions_report` | Attribution/visitor reporting with dates and filter/paging parameters; AC summary also requires `site_id`. |
| `get.reports_list`, `get.report_filters`, `get.source_filters` | Report/filter/source discovery; `get.report_filters` requires `report_type`. |
| `getobj.columns_tree`, `getobj.dimensions_tree` | Report-type column/dimension discovery. |
| `getobj.report_table`, `getobj.report_total` | Aggregate queries with report type, dates, columns, filter; table also dimensions, column filters and expansion. |
| `get.uploaded_calls`, `upload.calls` | Read uploaded-call report / write external historical call data. |
| `create.communication_comment`, `set.tag_communications`, `unset.tag_communications` | Communication mutations, not support-ticket replies. |

All rows above are inventory facts from the embedded snapshot, not live success claims. [Snapshot][spec]

Generated commands include `calls-report get`, `call-legs-report get`, `financial-call-legs-report get`, `reports-list get`, `report-filters get`, `report-table getobj`, and `report-total getobj`; structured params can be supplied as JSON. Runtime submits one logical request per invocation: no domain-level quick-search orchestration, complete-history pagination or archive is implemented there. A low-level `call <method>` accepts methods outside the snapshot, but that does not prove server support. [Generated commands][generated] [Runtime][runtime] [Raw call][raw]

Transport currently defaults to three retries and retries network failures and HTTP 429/502/503/504 without classifying the method as read or mutation. Consequently, existing raw/generated mutation support does not meet the send-once recommendation above; this is an implementation limitation, not evidence of provider idempotency. [Transport][transport] [Defaults][config]

Defaults are `dataapi.uiscom.ru` and `v2.0`. Host/version overrides exist, but there is no dedicated Call API or cabinet-session adapter in the inspected CLI. `auth login` stores an existing token; it is not UIS SSO or `login.user`. [CLI config][config] [CLI auth][auth] [Runtime][runtime]

There is a concrete schema-extraction limitation: nested request rows become flat parameters. For example, `getobj.report_table` contains repeated `filters`, `field`, `operator`, `value`, with inner `filters` marked required although outer `filter` is optional. Generated runtime checks every required entry at the top level. Therefore method presence is not sufficient evidence that all generated report commands accept a valid nested request without false missing-parameter failures. This is static-code evidence, not a live reproduction. [Snapshot][spec] [Scraper][scraper] [Generated runtime][runtime]

## SDK/spec/source assessment and follow-up boundary

The provider repository contains rendered documentation for Data, Call, Billing, Chat, JavaScript and interactive call-processing APIs, including pages absent from the UIS navigation. A recursive tree check found search-index JSON but no OpenAPI/Swagger JSON/YAML contract; no provider-maintained Go/Data API SDK was established. This is a bounded discovery result. Provider HTML tables/examples and existing Medivey adapters remain the usable primary contracts. [Pinned source tree][tree] [CLI scraper][scraper] [Medivey public adapter][m-uis]

Older Medivey research reported `comagic.github.io/data-api|call-api` links as unavailable. This research successfully fetched underscore paths `data_api/…`; do not carry the old broad absence claim forward. [Earlier reference research][m-research] [Provider source][provider]

Before implementation, resolve only these specific unknowns: UIS-host availability/permissions of the extra documented methods; correct `get.call_details` column IDs and required `entity_ids`; whether support tickets accept general incident contexts; saved-filter `report_type` type discrepancy; effective account quotas/prices; export download semantics; recording/transcription retention. These remain unknown because no authenticated reads or mutations were performed and several provider examples conflict with tables. [Details][details] [Support][ticket] [Saved filters][saved-create] [Exports][export-list] [Pricing][extended]

[general]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/
[general-mirror]: https://comagic.github.io/data_api/
[call-general]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/call_api/
[provider]: https://github.com/comagic/comagic.github.io
[tree]: https://github.com/comagic/comagic.github.io/tree/e457deb1e828bafc2243390f421e5b9db9859737
[calls]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_calls_report/
[legs]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_call_legs_report/
[reports]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report_new/common/get_reports_list/
[filters]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report_new/filter/get_report_filters/
[table]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report_new/aggregated/getobj_report_table/
[totals]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report_new/aggregated/getobj_report_total/
[support]: https://www.uiscom.ru/podderzhka/
[calls-ui]: https://www.uiscom.ru/academiya/spravochnyj-centr/otchety/spisok-obrashcheniy-zvonki/
[basic]: https://www.uiscom.ru/producty/integracii/basic-api/
[extended]: https://www.uiscom.ru/producty/integracii/extended-api/
[tariff]: https://www.uiscom.ru/upload/dokumenty/tarifnye-plany-na-litsenziyu-ooo-komedzhik-uis-s-20-02-2026.pdf
[billing]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/billing_api/index.html
[details]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/flat/get_call_details/index.html
[flat-calls]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/flat/get_calls/index.html
[ticket]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/customer/create_tech_support_ticket/index.html
[contact-info]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/customer/get_customer_contact_info/index.html
[rapid]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/filter/get_report_rapid_filters/index.html
[saved]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/filter/get_custom_filters/index.html
[saved-create]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/filter/create_custom_filter/index.html
[saved-delete]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/filter/delete_custom_filter/index.html
[state]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/common/getobj_report_state/index.html
[state-set]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/common/set_report_state/index.html
[export-create]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/report_generator/create_report_generator_task/index.html
[export-list]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/report_generator/get_report_generator_tasks/index.html
[recognize]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/flat/create_call_session_recognition_task/index.html
[recognition]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/data_api/report_new/flat/getobj_call_session_recognition_info/index.html
[originate]: https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/call_api/create_call/start_employee_call/
[active]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/call_api/manage_call/list_calls/index.html
[record]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/call_api/manage_call/record_call/index.html
[transfer]: https://github.com/comagic/comagic.github.io/blob/e457deb1e828bafc2243390f421e5b9db9859737/call_api/manage_call/transfer_talk/index.html
[spec]: /Users/comp/sandbox/uis-cli/internal/spec/data/api_spec.json
[loader]: /Users/comp/sandbox/uis-cli/internal/spec/spec.go
[scraper]: /Users/comp/sandbox/uis-cli/tools/uis-scrape/main.go
[make]: /Users/comp/sandbox/uis-cli/Makefile
[generated]: /Users/comp/sandbox/uis-cli/internal/cli/generated_resources.go
[runtime]: /Users/comp/sandbox/uis-cli/internal/cli/generated_runtime.go
[raw]: /Users/comp/sandbox/uis-cli/internal/cli/callcmd.go
[transport]: /Users/comp/sandbox/uis-cli/internal/client/jsonrpc.go
[config]: /Users/comp/sandbox/uis-cli/internal/config/config.go
[auth]: /Users/comp/sandbox/uis-cli/internal/cli/auth.go
[m-uis]: /Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis.ts
[m-private]: /Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis-private.ts
[m-call]: /Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis-call-api.ts
[m-voice]: /Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/docs/contexts/ai-operator/feature/ai-agent-voice.md
[m-research]: /Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/docs/contexts/crm/research/uis-call-events-and-employee-call-api.md
