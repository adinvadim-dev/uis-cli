# UIS private auth self-heal

Status: used 2026-11-11. No secrets. The operating agent repairs this itself; do not stop at a generic escalation.

This is **UIS CLI**, account `ACCOUNT`, binary `$HOME/.local/bin/uis`. A different CLI's session or 401 is not this recipe. Do not rotate another system's passwords for a UIS cabinet error.

## Detect

Treat as **auth** only on an explicit envelope, for example:

- `ok: false` and `error.code` `authentication_required`
- message like `Credentials or private session required`
- `error.details.recovery` containing `auth refresh --surface private --no-input`
- `error.details.stage` `cabinet_http`
- CLI exit 3 when the JSON says auth

Do **not** treat these as auth:

- process killed / exit 143 / SIGTERM / OpenClaw `timeoutSeconds` too short
- provider/transport timeout, HTTP 5xx, empty page, exit 6
- a 401 from a different CLI

`account doctor` and `auth status` only prove **files exist**. `session_present` / `cabinet_session_present: true` is not live report access.

Staff/cabinet reads can succeed while **private call search** fails: `staff employees list` uses `private.cabinet`; `calls search --source private` needs the report-constructor JWT (`private.report_constructor`). Repair on the failing command, not on a green status.

## Repair (once)

Use the configured account name and its `expected_login`, never an agent alias.

```sh
uis --account ACCOUNT --json account doctor
uis --account ACCOUNT --json auth status
```

If the profile is missing, credential_process is unset, or the binary is not `$HOME/.local/bin/uis`, name that blocker and stop. Do not invent another account.

On explicit auth error, **one** private refresh. Captcha via credential_process can take **minutes**. Use a long CLI timeout; do not SIGTERM at 20–45s.

```sh
uis --account ACCOUNT --json --no-input --timeout 180s \
  auth refresh --surface private
```

Default `auth refresh` / `auth login` surface is Data API and does **not** renew the cabinet JWT. Always `--surface private --no-input`.

If refresh fails closed (exit non-zero, `promoted` not true), **one** login with the same flags:

```sh
uis --account ACCOUNT --json --no-input --timeout 180s \
  auth login --surface private
```

`--no-input` uses credential_process + 2Captcha (`YandexSmartCaptchaTaskProxyless`). Headed browser only if AUTHENTICATION.md requires it; use the configured managed browser profile, not a personal one. Stop on passkey / password prompt / human OTP.

Do not refresh twice in the same incident. Do not use login as transport recovery.

## Re-read

Repeat the **same** failed private read (tiny window, `--retries 0`). Proof is JSON `ok: true` without secrets, plus `auth status` principal + `updated_at`.

Example probe:

```sh
uis --account ACCOUNT --json --timeout 60s --retries 0 \
  calls search --source private \
  --from "YYYY-MM-DD HH:MM:SS" --till "YYYY-MM-DD HH:MM:SS" --limit 1
```

## If still blocked

Tell humans the **exact** next physical step, not a generic escalate:

| Blocker | Next step |
| --- | --- |
| passkey / password / human OTP in headed login | owner completes that prompt in the configured managed browser profile |
| 2Captcha zero balance / unsolvable / no `captcha.2captcha_api_key` | top up or restore credential-process captcha field |
| credential_process missing `portal.login` / `portal.password` | restore `uis-credentials` helper; do not paste passwords in chat |
| principal mismatch vs `expected_login` | staged session discarded; owner checks UIS login identity |
| refresh/login still `authentication_required` after one cycle | paste sanitized `error.code` + `stage` + `recovery`; stop |
| timeout/SIGTERM during refresh | rerun **the same** refresh with ≥180s; do not assume auth is dead |
| a different CLI's 401 | that is not this recipe; use that CLI's own skill |

Never print tokens, JWTs, passwords, cookies, recording URLs, or SIP logins.

See [AUTHENTICATION.md](AUTHENTICATION.md) for the login ladder and captcha contract.
