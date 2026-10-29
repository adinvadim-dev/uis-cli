# uis

Go CLI for UIS calls, filtering, private tech support, and the generated Data API surface. The frozen contracts are in [docs/CLI_SPEC.md](docs/CLI_SPEC.md), [docs/AUTHENTICATION.md](docs/AUTHENTICATION.md) and [docs/archive-contract.md](docs/archive-contract.md).

**Private login and reads are implemented, tested with scripted HTTP servers.** Run `uis auth login --surface private --no-input` (or `refresh`) with an account `expected_login` and external `credential_process` providing portal credentials and `captcha.2captcha_api_key` when challenged. Login obtains separate app/Go tokens and promotes only after credential binding and a successful authenticated support read. This unlocks `support sync`, live ticket reads and private `calls show`. Support mutations remain fail-closed (exit 7): independent principal and CSRF verification for writes are unresolved. No live login, paid captcha or account mutation was used for development. See [implementation status](docs/IMPLEMENTATION.md).

## Build and verify

Requires Go 1.25+ and a C compiler (SQLite uses `go-sqlite3`, CGO required).

```sh
make build
go test ./...
go vet ./...
bin/uis --help
```

## Credentials

Profiles are non-secret TOML at `$XDG_CONFIG_HOME/uis/config.toml` (fallback `~/.config/uis/config.toml`), or `UIS_CONFIG`. No secrets through flags. Use an external helper producing exactly one `uis.credential-process/v1` JSON document; see the authentication contract. For automation `UIS_TOKEN` overrides the Data API field without invoking the helper.

```sh
uis account add team --expected-login user@example.com \
  --credential-command /absolute/path/to/uis-credentials --credential-command team
uis account use team
uis account doctor
uis auth login --surface dataapi
uis auth status
```

`auth login --surface dataapi` checks credential availability, not provider acceptance. `auth login|refresh --surface private` stages a fresh login and preserves the previous session on mismatch or any failure. Login labels are trimmed and matched case-insensitively; a configured customer-ID binding fails closed until a customer probe is observed. Legacy JSON Data API config remains readable; explicit account selection prevents reuse of a legacy stored token from another profile. `config set` only writes legacy JSON config, never TOML profiles. Prefer migrating stored tokens into the external helper.

## Calls and filters

```sh
uis --json calls search --phone 79000000000 \
  --from '2026-10-01 00:00:00' --till '2026-10-29 23:59:59' --lost
uis calls show 123 --date-hint 2026-10-29
uis calls legs 123 --from '2026-10-28 00:00:00' --till '2026-10-29 23:59:59'
uis filters list --report-type calls
uis filters rapid --report-type calls
uis filters show --report-type calls
uis reports list
uis --dry-run calls comment 123 --body 'Reviewed'
uis --force calls tag 123 --add 456
```

Search combines typed flags and an optional `--filter-json` object with AND. Dates default to the last 90 days. Details prefer an existing bound app session; fallback uses `get.calls_report`, verifies the requested ID, and marks `source`, `reduced`, and `fallback_reason`. A missing call returns exit 5. Call details never start login or request recognition. Recording/download URLs are stripped from output.

## Support and offline archive

```sh
uis support sync
uis --json support status
uis support ticket list --status created
uis support ticket show 123
uis support ticket search router
uis support ticket list --live --query incident
uis support topic list
uis support contact list
uis --dry-run support ticket create --topic-id 1 --contact-id 2 \
  --phone-id 3 --email-id 4 --body 'Please investigate' --attachment ./evidence.txt
uis --dry-run support ticket reply 123 --file ./message.txt
uis --dry-run support ticket close 123
uis --dry-run support ticket reopen 123
```

Local `list`, `show`, `search`, `status` need no network or credentials. Archive: `$XDG_STATE_HOME/uis/accounts/<account>/support.sqlite` (fallback `~/.local/state/uis/…`), permission 0600. SQLite contains sensitive ticket content. `support sync` is the network writer; `--live` reads directly and does not persist. Sync revisits all discovered tickets and all threads, commits each page with its cursor, upserts by provider identity, and advances freshness only after complete success. Partial runs remain degraded. Search uses SQLite FTS4 over ticket and comment content.

Attachments are metadata only (`preservation_status: pending`); download locators and session fields are excluded. Attachment bytes are not archived. The local SQLite implementation avoids crawlkit's newer toolchain requirement; migration to crawlkit is a follow-up.

## Safety and output

`--json` returns one `uis.cli/v1` envelope; `--plain` selects stable line-oriented human output. They are mutually exclusive. Diagnostics go to stderr. Messages accept exactly one of `--body`, `--file PATH`, or `--file -` (stdin); maximum 500 characters. Dry-runs validate locally and never invoke credentials, sessions, network or captcha; they may read the explicitly requested message file/stdin, but never attachment bytes.

Call annotations resolve the session’s `communication_id` through a read before dispatch; dry-run describes this lookup. All mutations, including generated and low-level methods, require TTY confirmation or `--force`. Automatic retries apply only to `get.*` / `getobj.*` reads. Ambiguous writes return `outcome_unknown` (9); never automatically resend. Private ticket creation is two writes, never invents an ID, and preserves the known ticket ID/stage on initial-comment failure. `--force` cannot bypass unavailable identity checks.

Exit codes: 0 success; 1 execution; 2 usage; 3 auth; 4 permission; 5 missing; 6 provider; 7 unavailable/drift; 8 quota; 9 unknown outcome; 10 confirmation. Earlier Data API codes 10–12 are replaced.

The existing generated commands, `uis call <method>`, `methods`, `spec`, `config`, and Cobra shell completion remain available. Use `uis <resource> <action> --help` or `uis call --help`. Generated metadata is maintained with `make spec gen`; do not hand-edit it.
