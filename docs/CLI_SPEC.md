# CLI_SPEC — `uis`

Status: frozen 2026-10-29 from to-cli research + user priorities (grilling skipped).

## Name

`uis` — command-line client for UIS / CoMagic: private cabinet support and call workflows, plus the public Data API.

## One-liner

Search and detail calls, manage UIS tech-support tickets, and call the Data API — safely for humans and agents.

## USAGE

```text
uis [global flags] <command> [subcommand] [args]
```

## Command tree

```text
uis auth login|status|logout|refresh
uis account add|list|show|use|remove|doctor
uis capability list|probe

uis support sync|status|doctor
uis support ticket list|show|search|create|reply|close|reopen
uis support topic list
uis support contact list

uis calls search|show|legs|comment|tag
uis filters list|show|rapid
uis reports list

uis balance show
uis recording download

# Existing generated Data API surface (kept)
uis <resource> <action> ...
uis call <method> [--param|--params-json]
uis config path|show|set
uis completion bash|zsh|fish|powershell
```

Commands may appear in help before their adapter is fully live. An unavailable live command fails closed with `capability_unavailable` (exit 7), never with a guessed request.

## Global flags

| Flag | Contract |
| --- | --- |
| `--account NAME` | Select local account profile. Falls back to `UIS_ACCOUNT`. No prompt for account name. |
| `--json` | Exactly one versioned JSON success or error envelope. Mutually exclusive with `--plain`. |
| `--plain` | Stable line-oriented text. Mutually exclusive with `--json`. |
| `--no-input` | Disable all prompts and interactive login. |
| `-n`, `--dry-run` | Local validation + redacted plan only. Zero network, credential_process, captcha, or session I/O. |
| `-f`, `--force` | Satisfy confirmation for a requested mutation. Never bypasses auth, capability, identity, drift, timeout, or redaction checks. |
| `--no-color` | Disable ANSI color (also off for non-TTY, `NO_COLOR`, `TERM=dumb`). |
| `--timeout DURATION` | Bound one network operation. Default `30s`; must be `>0` and `≤5m`. |
| `-q`, `--quiet` / `-v`, `--verbose` | Less / more stderr diagnostics. |
| `-h`, `--help` / `--version` | Help / version on stdout. |

## I/O contract

- **stdout:** primary data (human table/KV, `--json` envelope, or `--plain` lines).
- **stderr:** diagnostics, progress, confirmations prompts, errors when not using `--json` (JSON errors still go to stdout as the single envelope).
- Secrets, cookies, CSRF, bearer tokens, raw provider error bodies, phone numbers in verbose logs: never printed.

### JSON envelope

Success:

```json
{
  "ok": true,
  "schemaVersion": "uis.cli/v1",
  "command": "calls show",
  "data": {}
}
```

Error:

```json
{
  "ok": false,
  "schemaVersion": "uis.cli/v1",
  "command": "support ticket create",
  "error": {
    "code": "outcome_unknown",
    "message": "human-safe summary",
    "details": {}
  }
}
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Generic execution failure |
| 2 | Invalid usage / validation |
| 3 | Authentication required / expired |
| 4 | Permission denied |
| 5 | Not found |
| 6 | Provider / HTTP / API error |
| 7 | Capability unavailable / contract drift / unsupported |
| 8 | Rate limited |
| 9 | Mutation `outcome_unknown` (dispatch may have occurred) |
| 10 | Confirmation required (`--no-input` without `--force`) |

Legacy codes 10–12 from the earlier Data API client are remapped into this table; keep a compatibility note in CHANGELOG.

## Safety rules

- Classify by method semantics, not HTTP verb (Data API uses POST for reads).
- **Retries:** automatic retries only for classified reads. Mutations are sent once.
- After possible dispatch: bounded read-after-write; if ambiguous → `outcome_unknown` (exit 9), never blind retry.
- Destructive or externally disclosing mutations need TTY confirmation or `--force`.
- `--dry-run` never starts `credential_process`, captcha, browser, or network.
- Session refresh must not replay a compound mutation callback (do not copy Medivey’s `withUisPrivateSession` whole-callback replay around writes).

## Domain commands

### `auth` / `account` / `capability`

See [AUTHENTICATION.md](AUTHENTICATION.md).

### `support`

Private cabinet support (`infopin` on the go/comagic API host). Customer chats are out of scope.

| Command | Behavior |
| --- | --- |
| `support sync` | Network sync into local crawlkit archive (tickets + comments). |
| `support status` | Local freshness / checkpoints / degraded flags. |
| `support doctor` | Credential presence, session principal, archive health (no secret values). |
| `support ticket list` | **Local** by default (archive). `--live` forces network list. Filters: `--status`, `--query`, `--limit`, `--page`. |
| `support ticket show <id>` | Local by default; `--live` refreshes that ticket + thread. |
| `support ticket search <query>` | Local FTS over archived subject/body. |
| `support ticket create` | Two-step: create ticket then initial comment (+ optional `--attachment`). Requires topic + contact/phone/email IDs or interactive pickers. Message via `--body` / `--file` / stdin (exactly one). Max context 500 chars (cabinet UI limit; fail closed if exceeded unless server probe raises it). |
| `support ticket reply <id>` | Comment + optional attachments. |
| `support ticket close <id>` | PUT status close intent; reconcile stored status names. |
| `support ticket reopen <id>` | PUT reopen; respect 14-day UI rule as soft client check, report server refusal. |
| `support topic list` | Live topics (`tech-support-web-type`, available). |
| `support contact list` | Live contacts for ticket create. |

Create/reply/close/reopen: confirm or `--force`. Dry-run shows account, topic/contact IDs, attachment count — does not read attachment bytes.

Never fabricate ticket IDs (ignore cabinet `Date.now()` fallback). Require provider `id` before step-2 comment.

### `calls`

| Command | Behavior | Network |
| --- | --- | --- |
| `calls search` | Quick search: `--id`, `--phone`, `--from`, `--till`, `--direction`, `--lost`, `--transfer`, `--employee-id`, `--virtual-number`, `--scenario-id`, `--limit`, `--offset`, `--filter-json`. Uses Data API `get.calls_report` (and documented `get.calls` when entitlement probe succeeds). | Live |
| `calls show <session-id>` | Private `call_details_data` when private session available; else reduced Data API fallback with explicit `source` provenance. `--date-hint` optional. | Live |
| `calls legs <session-id>` | `get.call_legs_report` filtered by `call_session_id` within date window. | Live |
| `calls comment <session-id>` | `create.communication_comment` once; confirm text. | Live mutation |
| `calls tag <session-id> --add|--remove <tag-id>` | `set`/`unset.tag_communications`; confirm. | Live mutation |

Recording playback URLs stay inside adapters; `recording download` writes bytes to an explicit path after identity checks.

### `filters` / `reports`

| Command | Behavior |
| --- | --- |
| `filters list --report-type T` | `get.report_filters` |
| `filters rapid --report-type T` | `get.report_rapid_filters` when available; else capability_unavailable |
| `filters show` | Saved custom filters read (`get.custom_filters`) when available |
| `reports list` | `get.reports_list` |

Creating/deleting saved filters and `set.report_state` are **out of v1 command tree** (deferred mutations).

### Secondary reads

- `balance show` — private directory balance.
- Generated Data API resource commands remain available.

## Config / env precedence

flags > env > project config > user config > defaults

Env (non-secret):

- `UIS_ACCOUNT`
- `UIS_CONFIG`
- `UIS_HOST` / `UIS_API_VERSION` (Data API)
- `UIS_TIMEOUT` / `UIS_RETRIES` (retries apply to reads only)

Secrets never via flags. See AUTHENTICATION.md.

## Completion

`uis completion <shell>` prints a script to stdout.

## Examples

```bash
uis account doctor
uis auth login          # interactive private + data-api routing as configured
uis --json calls search --phone 79001234567 --from "2026-10-01 00:00:00" --till "2026-10-29 23:59:59"
uis calls show 5525810772
uis calls legs 5525810772 --from "2026-10-20 00:00:00" --till "2026-10-29 23:59:59"
uis support sync
uis support ticket list --status open
uis support ticket show 12345
uis --dry-run support ticket create --topic-id 1 --contact-id 2 --phone-id 3 --email-id 4 --file ./msg.txt
uis --force support ticket reply 12345 --body "Нужен статус по инциденту"
uis filters list --report-type calls
uis --json call get.calls_report --params-json '{"date_from":"...","date_till":"...","limit":10}'
```

## Deferred (named)

- Account-wide CDR / communications crawl archive
- Customer chat archive
- Support rating; creating contacts/phones/emails from support flow
- Sales tags; tag dictionary CRUD; recognition tasks
- Saved-filter create/delete; report-state writes; export/scheduled-export/Google Sheets
- Call API originate/transfer control plane
- Public `create.tech_support_ticket` as general support path
