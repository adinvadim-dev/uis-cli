# UIS authenticated web surface

Researched 2026-10-29. Scope: the authorized Medivey UIS cabinet, read-only research. No provider configuration, ticket, chat, call, blacklist, SIP resource, payment, or other business mutation was performed. No login was submitted and no paid CAPTCHA task was created.

## Outcome and evidence limits

A typed private adapter is feasible for the contracts already encoded in Medivey: call-session detail, balance, blacklist lookup, SIP URI listing, and separate billing reads. Support tickets/chat, additional call-detail endpoints, quick call search, and the cabinet communications-report endpoint remain **unknown**, not absent. Do not invent their paths from neighboring routes or treat public customer-chat reports as UIS technical support.

Live authentication was blocked at credential retrieval. Inside the persistent tmux `op-work` session, the one-password loader reported an available service token and `op_sa user get --me` confirmed `SERVICE_ACCOUNT`. Exact `op_sa item get` requests for both authorized titles, `UIS` and `Uiscom Медивей`, used `--vault "Service Vault"`; both returned **“Too many requests. Your client has been rate-limited.”** This is not evidence that the items do not exist. No other vault, account, production database credential, or saved authenticated profile was searched. No secret field values were obtained.

The agent-browser skill was read; live browser work used the harness's required `preview_*` controller. Navigating to `https://app.uiscom.ru/` reached the SSO login page. Only sanitized DOM structure, inline bootstrap schema, resource paths, and public SSO JavaScript signals were inspected. There was no authenticated network capture. No HAR was found in the target repository or the Medivey reference tree; a filename search of `/tmp` also yielded no relevant UIS HAR/cache. This search does not establish absence from all machine archives. The target CLI contains a cached public API specification, not a private cabinet contract archive.

Evidence labels used below:

- **Live unauthenticated:** current first-party page structure and bootstrap data, observed without submitting credentials.
- **Medivey code/test:** primary known private contracts from the requested reference, not a fresh live replay. Tests use mocked fetch; inspecting them does not prove today's server accepts the contract. Tests were read, not run.
- **Official public docs/cache:** a separate surface with separate authentication; never evidence of a private UI endpoint.
- **Unknown:** no observed contract sufficient for implementation.

## Primary local evidence

Medivey reference root: `/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c`; inspected HEAD `54025ac369b572c3dc460a3416743f3e24732026`. Line references below are relative to that root at this revision.

| Source | Relevant evidence |
| --- | --- |
| `server/utils/uis-private.ts:178` | Cookie jar; host-only/domain matching; token deletion |
| `server/utils/uis-private.ts:294` | Inline `APP_DATA` parser and expected SSO schema |
| `server/utils/uis-private.ts:440` | Legacy login bridge, redirect handling, CAPTCHA-backed SSO form |
| `server/utils/uis-private.ts:541` | Balance batch read |
| `server/utils/uis-private.ts:576` | Call details request and minimum response validation |
| `server/utils/uis-private.ts:608` | Separate billing SSO authorization |
| `server/utils/uis-private.ts:637` | Billing cookie transport, redirect-as-expiry handling |
| `server/utils/uis-private.ts:740` | Cached app session and refresh behavior |
| `server/utils/uis-private.ts:838`, `:916`, `:958` | Reduced public fallback and identifier provenance |
| `server/utils/uis-private-blacklist.ts:91` | Private blacklist query/filter contract |
| `server/utils/uis-private-sip-uri.ts:25`, `:55` | Typed SIP reads and bounded pagination |
| `test/unit/uis-private.test.ts:118`, `:171`, `:251`, `:291`, `:350`, `:379` | Mock contracts for redirects, SSO, details, separate billing token, daily charges, expiry |
| `test/unit/uis-private.test.ts:577`, `:640`, `:680`, `:743`, `:803` | Public fallback; exact report-ID and private-header identity matching |
| `test/unit/uis-private-sip-uri.test.ts` | Multi-page listing, malformed-response rejection, ambiguous-create handling |
| `docs/contexts/platform/feature/settings.md:194` | Intended private-session usage and reduced Data API detail fallback |
| `pages/sys/uis/call-session.vue:32`; `server/api/sys/uis/call-session.post.ts` | Medivey session lookup actually calls public `get.calls_report` |
| `components/uis/UisCallDetailsContent.vue:24`, `:35` | Recording references and HTML-like event text presentation |
| `server/api/cc/command-palette/calls/[id]/uis-call-details.get.ts:65` | Medivey command-palette wrapper delegates to the same private-detail/fallback helper |
| `shared/runtime/two-captcha.js:54`, `:82`, `:194` | Existing create/poll/native SmartCaptcha reference implementation |

The relevant private-client history includes `7e6357d7d` (billing charges), `5e0bf1de3` (network SIP URI provisioning), and `6f826a52d` (call-detail provenance). These are provenance aids, not current server compatibility guarantees.

## Authentication, cookies, CSRF, and principal

### Current login page: live unauthenticated observations

The navigation chain led to `sso.uiscom.ru/login`, with a continuation through `/oauth2/authorize`, `client_id=https://app.uiscom.ru`, and an app `/sso/callback`. The authorization continuation used `prompt=none`; the displayed login URL used `prompt=login`. No authorization codes or complete continuation URLs are retained here.

The login form declares POST to `/login`. Its input names are `username`, `password`, `not_trusted_device`, `project`, `csrf`, `identity_provider`, and `smart-token`. Inline script text contains JSON `APP_DATA`; it is not exposed as `window.APP_DATA` in this page. Reading the window property alone misleadingly returned no data; parsing the inline declaration succeeded. Its relevant schema currently includes:

```text
APP_DATA.project
APP_DATA.form_data.{csrf,identity_provider,not_trusted_device,username}
APP_DATA.captcha.{type,site_key}
```

Only presence/types were inspected: CAPTCHA type was `yandex`, a site key was present, and a CSRF value was present. No values are recorded. Other bootstrap keys include `page`, `password_policy`, `error_data`, `channels`, `locale`, and `trusted_reverse`; their meaning for authentication is unverified.

Loaded resources included SSO `/static/main.js` and split JS chunks, plus `smartcaptcha.yandexcloud.net/captcha.js`, advanced-challenge and backend iframes. This confirms Yandex SmartCaptcha, not reCAPTCHA. A same-origin read of the public SSO main bundle found `APP_DATA`, `csrf`, `identity_provider`, and `XMLHttpRequest` references. It did not expose authenticated app route contracts. `document.cookie` showed no readable cookie names; this says nothing about HttpOnly cookies, Set-Cookie attributes, or authenticated-session contents.

### Existing Medivey login contract: code/test evidence

1. POST `https://www.uiscom.ru/api/` with form encoding and fields `method=login`, `params[email]`, `params[password]`, and `params[token]=0token`. Headers include XHR, a login-page Referer, and a browser-like User-Agent. The literal legacy token field is an implementation detail, not proof that CAPTCHA can be bypassed.
2. Require `success`; follow `redirect_url` or the default app URL with a cookie jar and manual redirects. Every hop must be HTTPS on `uiscom.ru` or a subdomain; relative Location headers are resolved against the current URL. The loop allows eight iterations.
3. If an SSO HTML page appears, parse and validate `APP_DATA`, obtain a SmartCaptcha solution, then POST back to that SSO URL with form fields shown above and the jar's cookies. Fresh `csrf` and other bootstrap values must stay paired with that login attempt.
4. Obtain the app-host `access_token` cookie, then use it as a Bearer token for classic cabinet JSON endpoints. SSO and app cookies share names but have different host scopes; they are not interchangeable.

The jar supports domain versus host-only cookies and removes an entry for an empty token, quoted empty token, or `Max-Age=0`. It does **not** model cookie Path, Secure, HttpOnly, SameSite, or general expiry. It is an integration-specific example, not a full browser cookie implementation. The CLI should use a proper per-account cookie/session store rather than copying these limitations.

The app session cache stores an access token and update timestamp, without a documented TTL. Cached operations refresh after 401/403 once; other failures propagate. A fresh-session failure clears the cache on 401/403. This wrapper can replay its callback after auth rejection, so do not reuse its semantics blindly for mutations. Ordinary reads must distinguish authentication failure, permission denial, provider outage, and schema drift.

**Principal identity is an unresolved gating requirement.** These helpers do not verify a stable cabinet customer/account/user identity before accepting a token. A successful balance request establishes access to something, not access to the intended account. The public cached spec contains `get.account`, but that verifies the public API credential's principal; it does not independently bind a private browser session. No private `/me`/profile endpoint was observed. Stage a new profile, discover a read-only account identity source, compare it with the configured alias/account binding, and promote only after a match. Private principal mismatch, unsupported login flow, 2FA, or noninteractive expiry must fail explicitly, preserving any previously committed session.

### Separate billing session

`go.uiscom.ru` uses API host `uis-prod-msk-comagic-api.uiscom.ru`. The reference explicitly says the app token is rejected there. Starting with GET `/billing/service`, the logged-in SSO jar follows a separate `prompt=none` authorization flow and captures that API host's own `access_token` cookie. Subsequent billing requests send that cookie, `Origin: https://go.uiscom.ru`, Referer, and JSON Accept; they do not use the app Bearer header.

Billing requests use manual redirects: an SSO redirect is normalized to session expiry even if no 401/403 is returned. App and billing caches are separate. The current Medivey billing refresh performs full login and also refreshes the app cache. The actual lifetime, renewal token behavior, required Origin enforcement, and billing principal response remain unverified live.

## Known private reads and filters

All rows are **Medivey code/test contracts**, not provider promises or live authenticated observations. Hosts and route names are literal; placeholders contain no account data.

| Host and endpoint | Method and input | Expected response / paging | Boundary |
| --- | --- | --- | --- |
| `app.uiscom.ru/analytics/call/call_details_data/` | GET `call_id=<call-session-id>` | JSON `data.header` object and `data.grid` array; no paging in this adapter | Detail for a known call only, not a search/list endpoint; 20-second timeout |
| `app.uiscom.ru/directory/batch_reload/` | POST JSON `{"keys":["billing:public:balance"]}` | `success`; `data["billing:public:balance"]` array of `id/name` entries | Read despite POST; no other directory keys established |
| `app.uiscom.ru/services/ats__callblocking/blacklist/read/` | GET `site_id=-1`, `filter=<normalized-phone-digits>`, `page=1`, `start=0`, `limit=25` default, `sort=[{"property":"numa","direction":"ASC"}]` | `data` array; optional `total`; adapter filters exact normalized `numa` client-side | Search semantics may be broader than exact match; current helper reads first page only |
| `app.uiscom.ru/netresources/sipuri/records/read/` | GET `start=<offset>`, `limit=1000` | `success`, `data` array, optional nonnegative integer `total` | Increment offset by 1000; finish at total or short page without total; reject unexpected empty/incomplete progression and bound the loop |
| `uis-prod-msk-comagic-api.uiscom.ru/billing/total-sdr` | GET `date_from__ge=<YYYY-MM-DD>`, `date_till__le=<same-day>` | Bare array: `service_id` string, `cost` number, nullable `duration`, `date_from`, `date_till` | One Moscow calendar day; aggregate service charges, not per-call event detail; no pagination contract established |
| `uis-prod-msk-comagic-api.uiscom.ru/billing/service` | GET, no query in adapter | Bare array: string `id`, nullable `name/group_name`, nullable boolean `is_call` | Service classification catalog; no pagination contract established |

Classic requests send `Authorization: Bearer <session>`, `X-Requested-With: XMLHttpRequest`, and app Referer; some also send JSON Accept/Content-Type. No separate CSRF header is sent for these reads in the reference. This is evidence of the implemented request shape, not proof that arbitrary cabinet writes are CSRF-exempt. `site_id=-1` is an observed blacklist parameter; its universal meaning is not established. Do not copy it into other endpoints.

Balance mapping recognizes `current_balance`, `current_vbalance`, `current_currency`, `debt_limit`, `recommend_payment`, `money_enough_days`, `last_payment_date`, and `last_payment_sum`. Missing/incorrectly typed entries become null, so all-null balance must not be treated as proof of a healthy compatible contract.

SIP records are runtime-validated: positive integer `id`, nullable `name`, string `phone`, nullable `short_phone`. Blacklist records carry phone, comment, actor, timestamp, and outbound-blocking fields; retain these as sensitive data. The private helper normalizes `8`-prefixed eleven-digit phone numbers to `7` and strips nondigits. Its first-page/exact-match behavior is insufficient evidence of exhaustive blacklist export.

### Call details and identity semantics

The call header can contain `id`, recording references in `talk_file`, direction, start time, durations, employees, lost-call information, scenario, marks, and comment. Grid nodes include event time/type/mnemonic/text, failure information, duration, recursive `data`, and potentially `link_call_session_id`. These are candidate typed fields; most are optional and the existing parser validates only the outer header/grid minimum. Recording references are interpreted by the Medivey component as `record-id:duration`; this component renders labels and does not establish a recording-download endpoint. Event text can contain HTML-like formatting; Medivey strips tags for presentation. Linked IDs and nested events do not prove additional detail endpoints exist.

`getUisPrivateCallDetailsFromSettings` intentionally uses private details only when a private session already exists; otherwise it uses public `get.calls_report`. For durable handoff/outbound-attempt IDs it validates a positive safe integer and requires `header.id` to equal the requested ID before accepting private detail. Public fallback matches the report's `id`, not an assumed distinct `call_session_id`; it requests a date window around the hint or the last 90 days. A fallback response has synthetic start/talk/finish rows and `private_details_unavailable=true`; it is degraded data, not a private timeline. Keep provenance explicit in the CLI.

## Requested surfaces that remain unknown

| Research question | What was established | What still needs an authenticated read-only capture |
| --- | --- | --- |
| UIS technical support, tickets, support chat | No matching private endpoint in the examined helpers, UI, tests, or caches | Actual support route/backend, current principal/permissions, read side effects, list/detail/message/attachment shapes, status and date filters, pagination, and history completeness |
| Call detail beyond `call_details_data` | Nested grid events, recording references, linked call IDs in the current contract | Any separate legs, recordings, transcript, scoring, history, export, or timeline endpoint; do not infer URLs |
| Quick call search and call list | Medivey's own session lookup and command palette are wrappers around Data API/private-detail helpers | UIS cabinet search route, debounce/request behavior, phone/ID/date/direction/status/employee filters, sort and paging, timezone, count semantics |
| Communications-report UI | Public `get.communications_report` exists; no private UI request observed | Actual cabinet endpoint, report-definition IDs, table/grouping/filter serialization, export behavior, totals and pagination |
| Private-session principal | Login reaches the expected UIS host | Stable user/customer/account identifier, multi-account switching, role scope, cross-binding to billing |

Public “обращения” are customer communications, not necessarily support tickets. The current official [communications report documentation](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_communications_report/) describes `communication_type` values `chat`, `call`, `goal`, and `offline_message`; detailed records come from the corresponding reports. It documents date bounds, projection, filter, sort, limit/offset and agent `user_id`. This establishes a separate official alternative for customer communications, not a technical-support API or the implementation used by the cabinet UI. The cached CLI spec likewise includes `get.chats_report` and `get.chat_messages_report`; their names must not be used to claim support access.

For a future capture, open only read-only views and inspect the requests produced by changing one filter at a time. Do not enter/send a support message or create a ticket for discovery. Even opening a chat may mark messages read; inspect loaded app code before activating a widget where read receipts are possible. Reduce captures to method, host/path, parameter names/types, response schema, status, totals/cursors, and account-match booleans; never persist raw request headers, cookies, bootstrap secrets, bodies, names, phone numbers, or customer data in this report.

## Contract drift and implementation recommendations

Keep app, SSO, billing, and public Data API clients separate behind typed adapters. Existing evidence is sufficient to specify known-call detail and the listed reads; requested unsupported surfaces should return capability-unavailable/unknown, not guessed requests or DOM scraping.

Drift probes should be small and read-only. Validate trusted redirect destinations, SSO bootstrap schema and recognized CAPTCHA type, expected JSON content rather than a login HTML response, HTTP status plus logical `success`, required identity/schema fields, account binding, pagination progress, and requested-call/header-ID agreement. Treat an unexpected redirect on a JSON endpoint as a session/drift signal. Distinguish changed schema from empty results. Never log provider error bodies verbatim.

Do not carry over permissive `readJson<T>` casts as runtime validation. Make detail schemas bounded but tolerant of extra fields, retain unknown fields only inside a protected data boundary if needed, and ensure rendered output excludes HTML and secret/account bootstrap content. Add maximum page counts, record limits, timeouts and cancellation independent of server totals. No collection completeness claim is justified for first-page blacklist lookup, single-call detail, or undocumented billing paging.

The SIP adapter's code-derived create/delete routes and blacklist create/update/delete routes exist, but were not invoked; their existence does not establish support-ticket mutations. Action semantics belong to the separate actions research track. In particular, session refresh wrappers must not blindly replay an already dispatched write.

## Native 2Captcha adapter: current first-party API, brief

Verified against first-party docs on 2026-10-29; no solver credential was read and no task was submitted.

POST JSON to `https://api.2captcha.com/createTask` with `clientKey` and a `task` object; success returns `errorId: 0` and `taskId`. Treat task submission as a paid external action; do not create duplicate tasks after an ambiguous timeout. [createTask reference](https://2captcha.com/api-docs/create-task).

For UIS's observed challenge, select `YandexSmartCaptchaTaskProxyless`; provide `websiteURL`, `websiteKey`, and optionally the matching `userAgent`. `YandexSmartCaptchaTask` instead requires proxy type/address/port, optionally proxy authentication. Ready output uses `solution.token`, consumed through the site's `smart-token` field. Although the provider supports a cookies option, do not send UIS cookies. Image-based SmartCaptcha and puzzle tasks also exist; they are separate contracts and not needed for the observed token flow. [Yandex SmartCaptcha specification](https://2captcha.com/api-docs/yandex-smart-captcha).

POST `clientKey` and `taskId` to `https://api.2captcha.com/getTaskResult`. Handle `processing` versus `ready`, waiting at least five seconds between processing polls; reject ready responses with no valid token. Use a wall-clock deadline and cancellation, not just an attempt count. Medivey currently waits five seconds for up to 24 polls, but network time adds to that duration. [getTaskResult reference](https://2captcha.com/api-docs/get-task-result).

Map invalid key, zero balance, no capacity, unsolvable task, bad proxy, unsupported task, malformed parameters, HTTP failure, cancellation and timeout into typed outcomes. Provider errors can arrive in HTTP 200 with nonzero `errorId`; check both layers and sanitize descriptions. [Error-code reference](https://2captcha.com/api-docs/error-codes).

Adapter policy: resolve the solver key lazily through the authorized credential route; send only necessary public challenge metadata, never credentials, session cookies, CSRF values, account content, or private continuation parameters. The API expects the page URL: inspect it for secrets before sending it; if a safe equivalent URL cannot be established, use the human fallback. Match the browser User-Agent when supported, inject/submit within the staged login session, and verify the principal before promotion. Token expiry and any IP-binding requirement for this UIS account were not tested; consume promptly and never assume a fixed lifetime. No external solver CLI is required.
