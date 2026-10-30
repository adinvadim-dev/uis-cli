# AUTHENTICATION — `uis`

Status: frozen 2026-10-29.

## Boundary

`uis` never embeds a vendor secret store. Account profiles are non-secret TOML. When a network adapter needs a secret field, the CLI runs the profile’s external `credential_process` and parses one strict JSON document from its stdout. Help, local validation, `--dry-run`, and pure local archive queries never start `credential_process`.

## Profiles

User config default path: `$XDG_CONFIG_HOME/uis/config.toml` (fallback `~/.config/uis/config.toml`), override with `UIS_CONFIG`.

```toml
default_account = "medivey"

[accounts.medivey]
alias = "medivey"
# Expected principal labels for promotion checks (non-secret)
expected_login = "user@example.com"
# optional: expected_customer_id = "…"

[accounts.medivey.credential_process]
command = ["op", "run", "--", "uis-cred", "medivey"]
```

Commands:

```text
uis account add|list|show|use|remove|doctor
uis --account NAME …
```

`doctor` reports presence of credential fields and session files, never values.

## Credential-process contract

```json
{
  "schemaVersion": "uis.credential-process/v1",
  "fields": {
    "dataapi.access_token": "<optional>",
    "portal.login": "<optional>",
    "portal.password": "<optional>",
    "captcha.2captcha_api_key": "<optional>"
  }
}
```

Field rules:

- Adapters request only the fields they need.
- Missing required field → exit 3 with actionable message.
- Unknown fields ignored.
- Stdout must be exactly one JSON object; stderr from the helper is not parsed as credentials.

Env overrides for automation (still never flags):

- `UIS_TOKEN` → Data API token (overrides process for dataapi only)
- Prefer process for portal password and captcha key

## Surfaces and sessions

| Surface | Host(s) | Credential | Session artifact |
| --- | --- | --- | --- |
| Data API | `dataapi.uiscom.ru/v2.0` | `dataapi.access_token` | none (token per request) |
| App cabinet | `app.uiscom.ru` | `portal.login` + `portal.password` (+ captcha) | staged then promoted cookie/token store |
| Go / infopin / billing | `uis-prod-msk-comagic-api.uiscom.ru`, `go.uiscom.ru` | same portal login via separate SSO `prompt=none` | separate billing/support session store |

App Bearer tokens are **not** assumed valid for infopin/billing. Support commands use the go/infopin session.

Session files live under `$XDG_STATE_HOME/uis/accounts/<name>/` (fallback `~/.local/state/uis/…`) with restrictive permissions. They hold cookies/tokens only — never passwords.

## Login ladder (`uis auth login`)

1. Resolve account profile (no secret yet).
2. For Data API-only workflows: obtain `dataapi.access_token` and optionally verify with a tiny read (`get.account` when available).
3. For private workflows:
   - Stage a **new** session directory.
   - Run legacy login POST → trusted redirects → SSO SmartCaptcha solve (2Captcha `YandexSmartCaptchaTaskProxyless` or headed human fallback) → capture app `access_token`.
   - Derive go/infopin session via separate SSO client as Medivey billing does.
   - Discover a stable principal (login/email/customer id from a read-only endpoint or balance/support bootstrap). Compare to `expected_login` / configured binding.
   - On match: promote staged session atomically. On mismatch/timeout/drift: discard stage; keep previous promoted session.
4. `uis auth status` shows account, principal labels, session ages, captcha helper presence — no secrets.
5. `uis auth refresh` re-stages; `logout` deletes promoted sessions only after confirmation or `--force`.

`--no-input` login requires credential_process + captcha key (or fails closed). Headed human captcha is interactive-only.

## Captcha adapter

- Lazy-load `captcha.2captcha_api_key`.
- Send only public challenge metadata (`websiteURL`, `websiteKey`, user-agent). Never send UIS cookies, CSRF, passwords, or account bodies.
- CreateTask once; poll getTaskResult with ≥5s delay and wall-clock deadline.
- Ambiguous paid-task outcome: do not create a duplicate task; fail with structured error.
- Map provider `errorId` and HTTP failures to typed outcomes.

## Principal and drift

Before any private mutation:

- Require promoted session for the selected account.
- Re-check principal binding.
- Treat unexpected HTML/login redirect on a JSON endpoint as auth/drift → exit 3 or 7.
- Distinguish 401 (auth) from 403 (permission) — do not auto-replay mutations on either.

## Redaction

Logs and `--verbose` must redact: tokens, cookies, CSRF, `smart-token`, passwords, credential-process stdout, authorization headers, raw support message bodies in crash dumps unless explicitly writing to a user-requested file.
