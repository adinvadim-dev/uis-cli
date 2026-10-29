# Archive contract — UIS support

Status: frozen 2026-10-29.

## Scope

Local crawlkit archive for **UIS tech-support tickets and their comment threads** (account ↔ UIS support). Explicitly **not** customer website chats, call CDR history, or recordings.

Calls search/detail remain live-only in v1 ([to-cli-synthesis.md](research/to-cli-synthesis.md)).

## Ownership

| Layer | Owns |
| --- | --- |
| crawlkit | store open/tx, cursors/freshness, FTS helpers, control/progress DTOs, doctor metadata |
| uis adapter | infopin HTTP, auth/session, ticket/comment schemas, pagination meaning, completeness evidence, redaction |
| CLI | `support sync|status|doctor` and local `ticket list|show|search` |

If the pinned crawlkit tag requires a Go toolchain newer than this repo allows, ship the support live commands first and keep this archive interface behind a build tag / stub that returns `capability_unavailable` until the toolchain bump lands. Do not silently replace archive queries with live fetches while advertising “local”.

## Collections

### `support.tickets`

- Stable key: `(account_id, ticket_id)` where `ticket_id` is the provider `id` (never a client-synthesized timestamp).
- Mutable fields (status, status_name, dates, context snapshot) stored as latest observation + observation time; optional revision history encouraged.
- Discovery checkpoint: opaque adapter cursor / page offsets pinned with filter scope (`status`, `_q`, category).
- Completeness: terminal page for declared scope OR explicit degraded flag with failed-page list.

### `support.comments`

- Stable key: `(account_id, comment_id)` with FK `ticket_id`.
- Ordered by provider `date_time ASC`.
- Filter sync by `tech_support_id` + `is_ticket`.
- Exhaust each known ticket’s comments independently; a failed thread degrades the run even if ticket discovery finished.

### `support.attachments` (metadata first)

- Store provider file `id`, `filename`, `size`, `url` locator, parent comment/ticket.
- Bytes are optional enrichment: content-hash asset store outside SQLite; publish bytes before durable link.
- Metadata without bytes ≠ preserved attachment. Statuses: `pending|available|expired|failed|forbidden`.

## Sync semantics

- `uis support sync` is the only default network writer into the archive.
- Commit page records + next cursor in one transaction.
- Never advance final freshness after partial failure.
- Revisit open / recently updated tickets so replies to old tickets are not missed when discovery is creation-time ordered.
- Idempotent upserts by stable keys.

## Local queries

`ticket list|show|search` without `--live`:

- Use only the local DB.
- Make **no** network or credential_process calls.
- Surface `last_successful_sync` and degraded flags in human and JSON output.

`--live` may refresh one ticket or page, then upsert.

## Control metadata

`uis support status --json` and doctor expose a `crawlkit.control.v1`-compatible manifest for ticket collections: readiness, freshness, checkpoints, error counts — no secrets.

## Privacy

- Archive may contain customer PII and credentials pasted into tickets. Treat the DB as sensitive.
- Session cookies and download bearer URLs stay out of archival payloads.
- Default CLI output may truncate message bodies in list views; `show` prints full local body.
