---
name: uis-cli
description: "Use for UIS calls, filters, support tickets and Data API commands."
---

# UIS CLI

Build with `make build`; binary `bin/uis`. Read [README.md](README.md) for examples and [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md) before private operations.

Use `uis --json` for one versioned success/error envelope. `--account NAME` selects a non-secret TOML profile; credentials come from external `credential_process` or `UIS_TOKEN`. Run `account doctor` / `auth status` for presence checks. Never pass secrets in flags, print session files, or log provider bodies.

- Calls: `calls search --source private` with phone/date/direction/lost/transfer predicates; use `--employee` / `--scenario` with names from private filter discovery. Numeric employee/scenario IDs require `--source dataapi`. Private results identify cabinet `communication_id`, source and bounded page; `show ID` with explicit private/Data API source; `legs ID`; `comment ID --body TEXT`; `tag ID --add ID|--remove ID`.
- Discovery: `filters list|rapid|show --source private --report-type calls`, `reports list --source private`. Cabinet JWT login enables these reads without a Data API key.
- Support: `sync`, `status`, `doctor`; local `ticket list|show|search`; `--live` for direct reads; `topic list`, `contact list`; `ticket create|reply|close|reopen`.
- Generated resources and low-level `call METHOD --params-json JSON` remain available.

Private login/refresh: require `expected_login` and credential_process fields `portal.login`, `portal.password`; lazily request `captcha.2captcha_api_key` for SmartCaptcha. Use `auth login --surface private --no-input` before private search/discovery, `support sync`, `--live` ticket reads or private `calls show`. Login binds normalized credentials plus a successful authenticated read; it does not independently identify the provider principal. Production support mutations still return `capability_unavailable`: write principal/CSRF verification is not proven. Headed human captcha is follow-up; noninteractive challenges require 2Captcha. Do not bypass this boundary or assume an app bearer token works for support. No live mutation is authorized merely by invoking this skill.

Local archive reads must work without credentials/network. `support sync` is the writer. SQLite archives are sensitive; attachment metadata does not preserve bytes.

Before a mutation, inspect `--dry-run`; send only with explicit user authorization, TTY confirmation or `--force`. Dry-run reads a requested message file, but never attachments or credentials. On exit 9 (`outcome_unknown`) retain returned ticket ID/stage and reconcile with reads; never resend automatically. Ticket creation requires a provider ID before its separate initial comment. Do not enable sales, recognition, saved-filter writes, exports, customer-chat/CDR archives, or Call API originate.
