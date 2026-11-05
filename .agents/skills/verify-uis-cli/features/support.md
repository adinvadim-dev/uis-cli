# Support archive

Users refresh support history, inspect/search it offline, and separately authorize messages to UIS support when required.

## Sub-features

- `support-read`: live ticket/thread/topic/contact reads.
- `support-archive`: sync, freshness/degraded status, local list/show/FTS search.
- `support-write`: optional create/reply/close/reopen lifecycle with durable journal.

## How to get to it (user POV)

- `uis support sync`, `uis support status`.
- `uis support ticket list|show|search`; `--live` selects provider reads.
- `uis support topic list`, `uis support contact list`.
- `uis support ticket create|reply|close|reopen` requires specific external-message authorization.

## Driving it with production E2E

Preconditions: the calls/core-suite preconditions plus at least one support ticket. Live messaging is not authorized by verification-skill invocation.

- **Read and archive.** Run the core-suite command in `calls.md`. Its support/archive subtests discover a ticket, sync into private temporary state, check fresh/not-degraded status, then disable credential availability and verify offline list/show/search of that ticket.
- **Observe.** Require exact identity/content assertions from the harness and exit 0. Original user archive/session must not be deleted; only copies are used. Keep the sanitized suite log in `$VERIFY_RUN/evidence/`.
- **Write lifecycle.** Report unverified unless separately authorized. When authorized, follow `docs/PRODUCTION_E2E.md`'s exact independent opt-ins, fixture validation, persistent journal and recovery rules. Never add write flags to the baseline suite to silence a skip.

## Gotchas

- Local archive initialization differs from provider freshness; successful SQLite opening alone proves neither sync completeness nor a live read.
- Interrupted write journals survive and must be reconciled before another attempt. Cleanup must not erase them.
- Support creation sends a real ticket and initial message; there is no automatically disposable production fixture.
