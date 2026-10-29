---
name: uis-cli
description: "Use for UIS calls, filters, support tickets and Data API commands."
---

# UIS CLI

Build with `make build`; binary `bin/uis`. Read [README.md](README.md) for examples and [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md) before private operations.

Use `uis --json` for one versioned success/error envelope. `--account NAME` selects a non-secret TOML profile; credentials come from external `credential_process` or `UIS_TOKEN`. Run `account doctor` / `auth status` for presence checks. Never pass secrets in flags, print session files, or log provider bodies.

- Calls: `calls search` with phone/date/direction/lost/transfer/employee/scenario predicates; `show ID` with explicit private/Data API source; `legs ID`; `comment ID --body TEXT`; `tag ID --add ID|--remove ID`.
- Discovery: `filters list|rapid|show --report-type calls`, `reports list`.
- Support: `sync`, `status`, `doctor`; local `ticket list|show|search`; `--live` for direct reads; `topic list`, `contact list`; `ticket create|reply|close|reopen`.
- Generated resources and low-level `call METHOD --params-json JSON` remain available.

Private login and production support mutations currently return `capability_unavailable`: principal/CSRF verification is not proven. Do not bypass this boundary or assume an app bearer token works for support. No live mutation is authorized merely by invoking this skill.

Local archive reads must work without credentials/network. `support sync` is the writer. SQLite archives are sensitive; attachment metadata does not preserve bytes.

Before a mutation, inspect `--dry-run`; send only with explicit user authorization, TTY confirmation or `--force`. Dry-run reads a requested message file, but never attachments or credentials. On exit 9 (`outcome_unknown`) retain returned ticket ID/stage and reconcile with reads; never resend automatically. Ticket creation requires a provider ID before its separate initial comment. Do not enable sales, recognition, saved-filter writes, exports, customer-chat/CDR archives, or Call API originate.
