# UIS data and crawl boundary

Research date: 2026-10-29. Scope: support history, call detail, quick search and filters. Research only; no implementation, credentials, account exports, live mutations, or production database reads.

## Recommendation

Build a mixed product: **a crawlkit archive for UIS support tickets and their message threads; explicit live commands for call lists, filters, call detail and recording retrieval.** Support archive feasibility is conditional on the authenticated research track identifying its actual source contract. Do not pretend the existing Data API chat/communications methods are account-support methods.

Explicitly defer an independent, account-wide calls/CDR/communications archive and bulk recording mirror in uis-cli's first scope. This is a deliberate exception to to-cli's default of archiving useful durable collections, requiring confirmation at the parent product-boundary gate. Medivey already owns substantial call ingestion and delayed read models. Duplicating those pipelines is a large commitment relative to the immediate need for diagnosis and filtered lookup. This is a recommendation, not a claim that Medivey has complete UIS history or that the user has already approved the deferral. [P1, M1–M7]

| Collection or projection | Initial boundary | Explicit consequence |
| --- | --- | --- |
| Account support tickets, status snapshots, thread messages | Local crawlkit archive after source verification | Sync is network activity; archived list/search/show/export are local and show freshness. Unsupported source discovery blocks the archive, rather than silently substituting website visitor chats. |
| Support attachments | Asset enrichment of that archive | Separate pending/available/expired/failed coverage; attachment metadata does not prove byte preservation. |
| Calls report / filterable call list | Direct live, bounded reads | No offline search, independent retention guarantee, or account-wide completeness claim. |
| Full call detail / CDR legs | Direct live, by exact identity and bounded time context | Report-derived fallback must be labelled; it is not the native private event timeline. |
| Recording metadata and selected audio download | Direct live read/download | Bulk preservation is deferred; downloaded bytes may be exported explicitly, but a playback link is not an archive. |
| Unified communications report | Direct live, separately named | Customer communications are not support tickets; broader chat/email history is deferred. |
| Local call search index, transcript index, all-call raw-response history | Deferred | Add only with an agreed independent archive need or a verified Medivey read API. Never imply local search over live-only data. |

Keep UIS direct access independent of Medivey deployment and credentials. A future Medivey search integration can be a separately identified source, with its own lag and coverage; do not read its databases directly or silently merge results into a supposedly authoritative UIS response. Medivey's architecture explicitly keeps UIS as provider truth and its archive as a delayed read model. [M1, M2]

## Evidence and confidence

The existing uis-cli is a Go Data API client, with an embedded method/parameter snapshot and generated commands. Its inspected HEAD is `5d8abd4905e341d735ad5dd208900e41c4ef17a9` (2026-02-15). The snapshot does not establish current response schemas or account entitlements. Official call, CDR, communications and chat-message documentation was also fetched during this research. Private call-detail evidence comes from Medivey source at `54025ac369b572c3dc460a3416743f3e24732026`, not a new authenticated portal observation. [U1–U5, M3]

Confidence is high for the library ownership boundary and documented call/CDR identity distinction; medium for adapting Medivey's observed private details contract to this CLI; unresolved for the actual support-ticket API, tenant permissions, account-specific retention and historical coverage. No available evidence proves absence of a support API. Absence from the embedded method snapshot is only a discovery gap. [U1, M3, C1]

## Durable collection inventory

### 1. Account support tickets, support messages and attachments

This means the customer's conversations with **UIS support**, not their customers' website/messenger chats. The documented communications report describes `chat`, `call`, `goal`, and `offline_message` records. It therefore cannot establish support-ticket coverage. [U4]

**Identities:** ticket ID, thread/conversation ID, message ID, attachment ID, tenant/customer identity and visibility scope are all unverified. Desired archive keys are provider tenant plus immutable ticket/message IDs; include parent ticket/thread identity where IDs are only locally unique. Do not use subject, display number, sender or timestamp as a proven stable key. Distinguish account identity from login identity, because two logins may see overlapping tickets or different permission scopes. These are proposed identity requirements, not observed UIS fields.

**Pagination and late arrivals:** no support cursor, limit, offset, updated-since field, ordering, total count, tombstone, status-transition history or reopen semantics has been verified here. Discover tickets and enumerate each thread independently. If the source only lists by creation date, replies to old tickets require revisiting known open/recently changed threads and periodic older reconciliation; a creation-time watermark alone cannot discover them. Preserve revisions or observations for mutable status/text, with message pagination checkpoints separate from ticket discovery. Never declare a closed ticket permanently immutable without provider evidence.

**Search:** local full-text search across subject and message body; structured filters for observed status, created/updated times, ticket ID and attachment presence. Assignee/category/priority filters are conditional on actual source fields. Preserve thread ordering and parent relationships; search hits must identify their ticket and message. FTS is appropriate initially; semantic embeddings are unnecessary for the stated priorities and would introduce another content-processing boundary.

**Assets and sensitivity:** attachment descriptors and downloaded bytes have separate identities and progress. Support messages and files may contain customer data or credentials supplied to support. Auth/session envelopes and bearer download URLs must stay outside normal archival payloads. Preserve lossless business records after defining this secret boundary; if a business field itself contains sensitive material, treat the archive as sensitive rather than inventing silent redaction. Attachment URLs are locators, not identity or evidence of saved content. [P1, P2]

**Completeness evidence:** terminal discovery for the declared account/status/date scope, independently exhausted threads, ticket/message counts when available, attachment queue totals, failed/forbidden surfaces, last observation and last successful sync. A failed thread makes the run degraded even if ticket discovery completed. Unknowns include deleted/merged tickets, hidden internal notes, email continuations, chat-widget sessions, old closed history, edits, attachments and alternate support portals. Report these as unknown until the authenticated track resolves them. No complete support archive can be promised yet.

### 2. Call sessions and call details

**Identities:** `get.calls_report.id` identifies the session; CDR `id` identifies a leg and `call_session_id` links it to the session. Use tenant plus session ID; legs use tenant plus CDR ID with a session foreign key. Keep report, leg, transfer and private-detail identifiers distinct. Medivey explicitly uses an `id` filter for calls-report lookup and warns that the report does not expose a separate filterable `call_session_id`; some legacy paths still attempt that optional field and have fallback logic. Never infer identity from a matching phone/time alone. [U2, U3, M3, M4]

**Pagination and late arrivals:** calls and legs use date-bounded limit/offset reports, not a demonstrated opaque change cursor. Medivey's calls client loops pages of 1,000 and stops at a short page or `metadata.filtered`; its archive adapter uses explicit page offsets and a 10,000-row page size. These are inspected client behaviors, not proof of snapshot isolation. For a future archive, pin the date window/filter/field selection/sort/timezone into checkpoint identity, use supported deterministic sorting, detect repeated pages/no progress, bound work, and replay overlapping windows because offset scans can shift while calls arrive. Tie-breaking support and boundary inclusivity need verification. [U1, M4, M5]

Recent records can acquire final state and recording references later. Medivey has three-hour rolling recovery, previous-Moscow-day replay and repair of records on existing transfers. That is concrete evidence that a one-pass import is insufficient for its workflows; it is not a UIS settlement SLA. A future local collector needs hot windows, targeted ID refresh, explicit older replay and separately reported reconciliation. [M6]

**Details:** the observed private route is `https://app.uiscom.ru/analytics/call/call_details_data/`. Medivey models a header and event grid, including recording references, employees and call stages. It can fall back to a Data API projection marked `private_details_unavailable`; that projection must not be presented as the original routing/event timeline. For future preservation, store detail observations separately from session inventory and retain source/provenance and observation time. There is no verified bulk private-detail cursor here. [M3]

**Search and retention:** fast operational lookup needs exact session ID, normalized phone and date window, plus supported direction, employee, status/outcome, duration, scenario, number, transfer and recording-presence filters. Whether each is server-filterable must follow the source's field matrix, rather than treating every returned field as filterable. Full-text transcript search is a separate deferred capability. Call metadata history and audio lifetime are different retention questions; the account's metadata horizon, historical deletion and current entitlements remain unknown. [U1, M4]

**Completeness evidence:** for live commands, report query scope, pages read, truncation, fetch time and upstream totals when present. A limited query succeeds as a limited result, not as “all calls.” For a future archive, require completed windows, distinct IDs, counts/parity checks, zero unhandled page failures and separate detail coverage. Count equality alone does not prove record parity. Medivey's own architecture distinguishes imported from reconciled buckets and expected hot-window lag. [M1]

### 3. Recordings metadata and audio assets

**Identities and multiplicity:** use tenant, session ID and recording ID; one session can have multiple recordings and legs. Preserve all references and their source roles instead of choosing the first as the complete call. Official reports expose recording IDs; Medivey derives media URLs using session/recording identity and has separate timeline logic because native and composite recordings differ. [U2, U3, M3, M7]

**Discovery and lateness:** there is no verified independent recordings cursor here. Discover references through sessions, legs and private detail, refresh sessions with missing media, and track fetch attempts separately. Missing references, a configured non-recorded call, temporarily unavailable audio, expired audio and permission failure must remain distinct where evidence allows. Do not turn a transient failure into permanent “no recording.”

**Binary ownership:** if bulk archive is later approved, keep immutable bytes by content hash outside SQLite, with digest, byte length, media type, source recording identity and download observation. Publish bytes before committing their durable reference. Store normalized metadata/raw business records in SQLite. The content-addressed asset store is an application responsibility illustrated by diacrawl, not a crawlkit recording-downloader API. Validate media and redirect destinations rather than trusting arbitrary URLs in source records. [P2, C1]

**Retention:** UIS documents configurable recording rules and tariff-dependent retention; its download guide says links expire with recording retention. A URL-only archive cannot guarantee future playback. The published example of a 30-day tariff is not this account's retention setting. Initial direct download therefore explicitly gives no automatic preservation guarantee. If durable audio becomes a priority, this deferral must be revisited before expiry. [U6, U7]

**Search/completeness:** recording presence/availability is metadata, not content search. Audio transcript indexing requires a separately approved transcript source and coverage. Report known recording references, preserved bytes, pending/failed/unavailable assets and per-call multiplicity. Audio completeness cannot be inferred from completed call pagination.

### 4. Communications, chat messages and search indexes

The general communications report is a heterogeneous customer-contact list; prefer a conservative key of tenant, communication type and provider ID until cross-type global uniqueness is proven. `get.communications` is a separate report-specific method requiring report identity/type in the embedded snapshot; do not assume it is interchangeable with `get.communications_report`. Report configuration and field/filter selection belong in provenance. Both expose offset/limit-style interfaces in the snapshot. Late tags, comments and attribution can change records already discovered; event-time watermarks alone would not preserve their latest state. [U1, U4]

Documented chat messages have message and chat IDs, ordered message time and attachment resource descriptors. Their API supports per-chat selection and date windows of at most 30 days, plus offset pagination. A future customer-chat archive needs separate chat discovery and message checkpoints, stable deduplication and attachment coverage. This is useful durable data, but it is explicitly deferred because customer omnichannel history is broader than the stated account-support priority. [U5]

A search index is a rebuildable projection of archived records, not another provider collection with its own authoritative cursor. Local FTS should follow the archive transaction/revision; normalized phone/ID and structured indexes serve precise filters. Live UIS calls filtering remains a network query with provider latency and rate limits. Do not advertise offline call search, full-text call search or complete unified communication history in the initial scope. Unknown surfaces include email bodies, messenger edits/deletes, goals, offline-message attachments, speech analytics/transcripts, private saved report definitions and hidden permission-filtered data. [U1, C2]

## What Medivey already owns, and what it does not prove

| Inspected surface | Existing behavior | Boundary implication |
| --- | --- | --- |
| Employee `calls` PostgreSQL table | Unique UIS call ID, one stored employee ID, phone/time/outcomes and recording references; employee sync fetches at most 10,000 calls before filtering | Useful operational projection, not a proof of all account calls, all participants or lossless raw history. [M4] |
| Transfer sync | Domain classification/correlation, persistent transfer links, late recording repair, rolling/D-1 replay | Business admission semantics belong to Medivey; uis-cli should not duplicate transfer decisions. [M6] |
| Calls archive importer | Explicit offset checkpoint, normalized archive rows and optional raw-payload reference; mapper picks one recording reference | Existing durable archive work, but importer presence alone proves neither deployed coverage nor full audio/raw preservation. [M5] |
| Finance CDR/financial-legs mirror | Delayed joined read model; hourly import owns lag, retry and evidence | Avoid a parallel finance history pipeline merely for call diagnostics. [M2] |
| Private call details | Live private read with Data API fallback | Direct UIS detail commands remain useful even with internal persistence. [M3] |
| Recording cache/transcripts | Selected audio cached under call/record identity; transcript workflows for selected UIS calls | Does not establish an all-recordings archive or a general UIS transcript index. [M7] |

No production coverage measurement was performed. Missing employee sync pages, historic gaps, archive deployment state and current permissions remain unmeasured. The decision to defer duplication depends on priorities, not an unsupported “Medivey has everything” claim.

## Tagged crawlkit API and ownership

Inspected **v0.16.5**, tag commit `93228b16b6188e6547ca5fe11f4771e1aec2192f` dated 2026-09-22: highest version tag present locally. The checkout's `git describe` is `v0.13.4-4-g4f69fdb`, so inspecting only working-tree files would give a stale API inventory. The global latest release was not independently checked. All library API conclusions below come from `git show v0.16.5:<path>`. [C1–C4]

| crawlkit owns | UIS application/provider adapter owns |
| --- | --- |
| `config` paths/defaults and nonsecret diagnostics | Tenant/principal identity, credential/session resolution, permissions and private-contract probes |
| `store.Open`, `OpenReadOnly`, `WithTx`, schema-version/query helpers | UIS tables/migrations, normalized/raw business records, stable keys, privacy and erasure policy |
| `state.New`, `NewCursor`, `NewScoped` and mapped variants, cursor/freshness persistence | Pagination, date/time semantics, checkpoint meaning, ordering, overlap/replay and completion evidence |
| FTS5 query helpers such as `FTS5Terms` and `FTS5TokenQuery` | FTS schema/content, filters, phone normalization, ranking and index lifecycle |
| `control`, `output`, `progress` DTOs/helpers | Command tree, JSON contract, collection status, degraded/unsupported distinctions and `crawlkit.control.v1` manifest contents |
| Optional snapshot/backup/mirror sidecar mechanics | Asset downloader, hashing/publication, retention, export admission and whether any remote publishing is allowed |
| Optional generic worker execution bounds | Durable enrichment queue, eligibility, retries, source revision and result transactions |

`state.New` accepts a transaction-capable executor, and `store.WithTx` provides the transaction boundary: page records and their next checkpoint can be committed together. Crawlkit does not supply a UIS crawler that makes this atomic automatically. The adapter/run coordinator must arrange it, keep per-collection completion distinct from an empty/missing cursor and never advance final freshness after partial failure. [C1–C3, P2, P3]

Do not put UIS endpoint schemas, rate-limit handling, media host policy or support completeness in crawlkit. Do not import optional embeddings, hosted archives or synchronization features just because they exist. v0.16.5 also requires Go 1.27.0 with preferred toolchain 1.27.1, while uis-cli currently declares Go 1.25.0; adopting that tag entails a deliberate runtime/toolchain decision, not merely adding an import. [C4, U1]

## Decisions still needed from the parent research/synthesis

The authenticated track must identify support list/detail/message/attachment routes, account scope, pagination, immutable IDs, older/closed tickets, reply/edit/delete behavior and access-denied signals. Record observed private contracts separately from provider guarantees. Without this, support preservation remains an explicit blocked capability.

Confirm the named call/CDR/communications/audio archive deferrals at the product-boundary gate. If offline cross-history call search or independent preservation is required, add a scoped calls archive with its own windows, parity and detail/assets coverage; live filtering cannot satisfy that requirement. If reuse of Medivey is preferred, verify its supported API, authorization, search fields and freshness before proposing integration. Existing source code is not evidence of current production completeness.

## Sources

Local references are directly inspectable; tagged links identify the exact upstream version inspected. Official URLs were fetched during this research except where explicitly described as embedded snapshot evidence.

- **P1:** [to-cli reference patterns](/Users/comp/.agents/skills/to-cli/references/patterns.md) and [skill](/Users/comp/.agents/skills/to-cli/SKILL.md): direct/archive default and provider boundary.
- **P2:** [diacrawl architecture](/Users/comp/sandbox/diacrawl/docs/architecture.md), [coverage](/Users/comp/sandbox/diacrawl/docs/coverage.md), [asset store](/Users/comp/sandbox/diacrawl/internal/assets/store.go).
- **P3:** [exchangecrawl specification](/Users/comp/sandbox/exchangecrawl/SPEC.md) and [README](/Users/comp/sandbox/exchangecrawl/README.md): local queries, overlapping windows, checkpoints.
- **U1:** [uis-cli README](/Users/comp/sandbox/uis-cli/README.md), [embedded API snapshot](/Users/comp/sandbox/uis-cli/internal/spec/data/api_spec.json), [go.mod](/Users/comp/sandbox/uis-cli/go.mod).
- **U2:** [UIS calls report](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_calls_report/).
- **U3:** [UIS CDR/legs report](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_call_legs_report/).
- **U4:** [UIS communications report](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_communications_report/).
- **U5:** [UIS chat messages](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/report/get_chat_messages_report/).
- **U6:** [UIS recording rules](https://www.uiscom.ru/academiya/spravochnyj-centr/virtualnaya-ats/pravila-zapisi-razgovorov/).
- **U7:** [UIS downloading recordings](https://www.uiscom.ru/academiya/spravochnyj-centr/otchety/skachivanie-zapisey-zvonkov-v-rezhime-onlayn/).
- **M1:** [Medivey archive ownership ADR](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/docs/adr/0011-call-archive-read-model.md).
- **M2:** [Medivey legs mirror ADR](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/docs/adr/0029-outbound-run-spend-from-uis-legs-mirror.md).
- **M3:** [Private call-detail client and fallback](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis-private.ts), [shared detail types](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/shared/types/uis-call-details.ts).
- **M4:** [UIS public client](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis.ts), [employee sync](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/employee-calls.ts), [calls schema](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/database/schema/calls.ts).
- **M5:** [UIS archive importer](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/archive-providers/uis.ts). Inspected only for ingestion/projection ownership; no ClickHouse query or schema review performed.
- **M6:** [Recovery windows](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis-transfer-recovery.ts), [transfer persistence/repair](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis-sync-db.ts), [D-1 replay task](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/tasks/uis/replay-partner-calls-d1.ts).
- **M7:** [Recording media cache](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/server/utils/uis-recording-media.ts), [AI voice recording workflows](/Users/comp/.t3/worktrees/medivey-hq/t3-b3f9127c/docs/contexts/ai-operator/feature/ai-agent-voice.md).
- **C1:** [crawlkit v0.16.5 boundary](https://github.com/openclaw/crawlkit/blob/v0.16.5/docs/boundary.md), [package guide](https://github.com/openclaw/crawlkit/blob/v0.16.5/docs/packages.md).
- **C2:** [Tagged store API](https://github.com/openclaw/crawlkit/blob/v0.16.5/store/store.go), [FTS helpers](https://github.com/openclaw/crawlkit/blob/v0.16.5/store/fts.go).
- **C3:** [Tagged state API](https://github.com/openclaw/crawlkit/blob/v0.16.5/state/state.go), [cursor/scoped adapters](https://github.com/openclaw/crawlkit/blob/v0.16.5/state/adapters.go).
- **C4:** [Tagged go.mod](https://github.com/openclaw/crawlkit/blob/v0.16.5/go.mod). Local source repository: `/Users/comp/sandbox/oss/crawlkit`.
