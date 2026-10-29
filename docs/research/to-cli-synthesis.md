# to-cli synthesis: uis-cli

Research date: 2026-10-29. Grilling skipped by explicit user request (`без грилинга`). Product decisions below are frozen from that request plus the four research reports.

## User priorities (binding)

1. Everything about **tech support** and communicating with it.
2. Everything about **call detailing**.
3. Everything about **quick call search**.
4. **Any filtering**.
5. Center the product on the **private cabinet API** used in the personal account.
6. Reuse Medivey’s already-proven private API work where it exists.

## Capability matrix

| Capability | Source | Official/private | Free/paid | Auth | Read/action | Direct/crawl | Risk | Evidence | Confidence |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Support ticket list/show/search | Private `infopin/tech-support` on `uis-prod-msk-comagic-api.uiscom.ru` | Private (source-observed cabinet) | Account entitlement | go/SSO session cookies | Read | Crawl archive + live refresh | Medium: principal/CSRF/read-receipt unknowns | 04 S1–S6 | Medium |
| Support create (ticket + initial comment/files) | Same `infopin` multipart | Private | Account; topic may request billable change | go/SSO | Action (2 writes) | Direct | High: non-atomic two-step, no idempotency | 04 S5–S7 | Medium-high (wire shape) |
| Support reply / close / reopen | `infopin/tech-support-comment`, PUT status | Private | Account | go/SSO | Action | Direct | Medium-high | 04 S7–S8 | Medium-high |
| Support rating / contact create | Adjacent cabinet mutations | Private | Account | go/SSO | Action | Deferred | Medium | 04 S8–S9 | Medium |
| Narrow public `create.tech_support_ticket` | Data API docs (github.io) | Official docs | API plan | Data API token | Action | Deferred / fail-closed until verified | High: number-connect only, no ticket ID | 01 ticket | Low for general support |
| Private call details timeline | `app.uiscom.ru/analytics/call/call_details_data/` | Private (Medivey) | Account | app Bearer | Read | Direct | Medium: session drift | 02, Medivey | High (adapter) |
| Public call summary / legs / filters | `get.calls_report`, `get.call_legs_report` | Official | API plan | Data API token | Read | Direct | Low-medium quota | 01, existing CLI | High |
| Documented `get.call_details` / `get.calls` | Provider github.io | Official docs | API plan | Data API token | Read | Direct after probe | Medium: missing from CLI snapshot; host entitlement unknown | 01 | Medium |
| Filter discovery / rapid / saved filters | Report-new methods | Official + cabinet JS | API plan | Data API / cabinet RPC | Read + mutate (saved) | Direct reads first; saved-filter writes deferred | Medium | 01, 04 S10 | Medium |
| Report export / scheduled export | report-constructor | Private-ish cabinet | Account | report-constructor session | Action | Deferred | High disclosure | 04 S10 | Medium |
| Call comments / tags / sales | Public Data API | Official | API plan | Data API token | Action | Direct (comments/tags in v1; sales deferred) | Medium | 01, 04 P1–P7 | High docs |
| Balance / billing charges | Private directory + billing API | Private (Medivey) | Account | app + billing SSO | Read | Direct secondary | Low | 02 | High |
| Blacklist / SIP URI | Private app routes | Private (Medivey) | Account | app Bearer | Read/action | Secondary / out of priority core | Medium compound writes | 02, 04 M2–M7 | High Medivey |
| Customer chats / communications | Public reports | Official | API plan | Data API token | Read | Deferred (not UIS support) | Medium PII | 01, 03 | High docs |
| Account-wide CDR archive | Data API reports | Official | API plan | Data API token | Read | Deferred (Medivey owns heavy sync) | High scope | 03 | High recommendation |

## Conflicts and unknowns (explicit)

- Private support REST is **source-observed** from published frontend JS, not live-authenticated on this account. Principal binding, CSRF/cookie scope, and entitlement still need a read-only probe before claiming production readiness.
- Public `create.tech_support_ticket` is **not** general tech support; do not route `uis support ticket create` through it.
- Customer “обращения” / chats ≠ UIS support tickets.
- `get.call_details` / saved filters / rapid filters exist in provider docs but are absent from the embedded 148-method snapshot; probe before advertising.
- 1Password rate-limit blocked live LK login during research; auth ladder remains unproven end-to-end in this session.
- crawlkit v0.16.5 wants Go ≥1.27; local uis-cli is Go 1.25 and the machine has Go 1.26. Archive adoption must pick a compatible crawlkit tag or bump toolchain deliberately.

## Frozen product decisions (no grilling)

| Decision | Choice | Rationale |
| --- | --- | --- |
| Command name | `uis` | Existing repo/binary |
| Audience | Humans + agents | Tables/`--json`/`--plain` |
| Runtime | One Go binary | Matches current repo + regru/diacrawl pattern |
| Auth model | Multi-profile; secrets via `credential_process`; private sessions in per-account store | to-cli ladder + Medivey patterns |
| Primary surfaces | Private support (`infopin`) + private call details + public Data API search/filters | User priorities |
| Support archive | Yes — crawlkit-backed local archive for tickets/comments; `sync` is the network boundary | Durable conversation history |
| Calls archive | Deferred | Medivey already owns heavy call ingestion; live search/detail first |
| Customer chat archive | Deferred | Not UIS tech support |
| Support mutations | create (2-step), reply, close, reopen; rating/contact-create deferred | Core communication |
| Call mutations | comment + tag set/unset in v1; sales/transcription/tag-dictionary CRUD deferred | Detail annotations without revenue writes |
| Saved filters / report state / exports | Reads/discovery in v1; mutating save/export deferred | Filtering without preference side-effects |
| Existing generated Data API commands | Keep as broader surface / escape hatch | Already shipped |
| Transport retries | Reads only; mutations send once → `outcome_unknown` | Fixes current client bug |
| Captcha | Native 2Captcha Yandex SmartCaptcha adapter + headed human fallback | to-cli auth ladder |
| Medivey coupling | Independent CLI; adapt contracts from Medivey code, never read Medivey DB | Isolation |

## Implementation phases implied by contracts

1. Freeze CLI/auth/archive specs (this synthesis + `docs/CLI_SPEC.md`, `AUTHENTICATION.md`, `archive-contract.md`).
2. Implement private session + support + call search/detail + filter flags with tests.
3. Wire support crawlkit archive when toolchain allows; otherwise scaffold store interface and ship live support commands first.
4. Probe undocumented Data API methods with read-only entitlement checks; enable or fail-closed.

## Source reports

- [01-official-surface.md](01-official-surface.md)
- [02-authenticated-web-surface.md](02-authenticated-web-surface.md)
- [03-data-and-crawl-boundary.md](03-data-and-crawl-boundary.md)
- [04-actions-and-failure-semantics.md](04-actions-and-failure-semantics.md)
