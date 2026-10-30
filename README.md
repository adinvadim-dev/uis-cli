# uis

Go CLI for UIS calls, filtering, private tech support, and the generated Data API surface. The frozen contracts are in [docs/CLI_SPEC.md](docs/CLI_SPEC.md), [docs/AUTHENTICATION.md](docs/AUTHENTICATION.md) and [docs/archive-contract.md](docs/archive-contract.md).

**Private login, reads and support ticket mutations are implemented.** Run `uis auth login --surface private --no-input` (or `refresh`) with an account `expected_login` and external `credential_process` providing portal credentials and `captcha.2captcha_api_key` when challenged. Login obtains separate app/Go tokens and promotes only after credential binding and a successful authenticated support read. This unlocks `support sync`, live ticket reads and private `calls show`. Support writes re-check account binding and an authenticated support read; a dedicated provider principal/CSRF contract remains unresolved. Opt-in [production E2E](docs/PRODUCTION_E2E.md) covers fresh login, reads and a separately authorized test-ticket lifecycle. Private call search and discovery were verified live through the cabinet JWT without a Data API key; no business mutations were used for this call-search validation. See [implementation status](docs/IMPLEMENTATION.md).

## Build and verify

Requires Go 1.25+ and a C compiler (SQLite uses `go-sqlite3`, CGO required).

```sh
make build
go test ./...
go vet ./...
bin/uis --help
```

For opt-in checks against real UIS accounts, see [Production E2E](docs/PRODUCTION_E2E.md) and `make test-production`. The suite is separate from ordinary tests. It performs production reads; a separately enabled support-write scenario creates one test ticket, replies, closes/reopens it and leaves it closed. Other mutation coverage uses dry-run.

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

Search defaults to `--source auto`: an existing bound private session selects the cabinet report; otherwise the existing Data API path remains available. Use `--source private` for token-free cabinet search and `--source dataapi` for the original `get.calls_report` contract. An unavailable/expired private report session fails explicitly; refresh with `auth refresh --surface private --no-input`. Login captures separate app, support and cabinet JWT credentials. No Data API key is needed for private search or discovery.

```sh
uis --json calls search --source private --id 123 --from '2026-10-29 00:00:00' --till '2026-10-30 00:00:00'
uis --json calls search --source private --direction in --lost --transfer=false --limit 25 --offset 25
uis filters list --source private --report-type calls
uis calls search --source private --employee 'Exact cabinet name' --scenario 'Exact cabinet scenario'
uis calls search --source dataapi --employee-id 123
```

Private search combines flags and `--filter-json` with AND. It returns `source`, rows, date window, limit/offset and metadata; `total_items: null` means UIS omitted the count for an empty result. Private enum cells retain cabinet objects/values. ID means cabinet `communication_id`; employee/scenario names come from `filters list`, while their numeric ID flags require Data API. Phone matching is exact and strings are sent literally. Private sort supports `--sort call_start_date_time|communication_id --order asc|desc`. Raw private filters use cabinet fields/operators/values, which differ from Data API. Dates default to the last 90 days; they are literal cabinet wall time, with no verified timezone/retention guarantee. Offset pages have no snapshot/completeness guarantee.

Details prefer an existing bound app session; fallback uses `get.calls_report`, verifies the requested session ID, and marks `source`, `reduced`, and `fallback_reason`. A missing call returns exit 5. Call details never start login or request recognition. Recording/download URLs are stripped from output.


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

## Agent skill bundle

For call/transfer investigations, start with [the bounded recipe](docs/TRANSFER_INVESTIGATION.md).
The Python 3 standard-library helper `scripts/call_brief.py` projects search rows,
nested private timelines and one requested filter field without loading raw provider
bodies or account-wide catalogs into the model context. It preserves source, IDs,
unknown counts and explicit omission/truncation markers. It performs no network calls.

`make check` runs Go tests/vet, Python contract tests and recursive skill dependency
validation. `make install-hooks` installs the same local pre-commit gate; no GitHub
Actions checks are required. `make skill-bundle` creates a new `dist/uis-cli-skill`
directory containing `SKILL.md`, the helper and its linked documentation. An existing
output directory is refused: use a fresh output path with
`python3 scripts/skill_bundle.py --output /absolute/new-directory` for another build.
Install the complete bundle together; copying only `SKILL.md` leaves its recipe and
research references unavailable. Keep account-specific configuration outside it.

## Outgoing campaigns (autodial)

The [coverage inventory](docs/research/06-autodial-coverage.md) distinguishes outgoing campaigns from advertising `campaigns`. Four official methods are generated under `auto-call-campaigns`, `auto-call-campaign-phone-numbers`, and `auto-call-campaign-status`; `make spec` now also merges the provider's outgoing-campaign documentation.

```sh
uis --json autodial statuses
uis --json autodial get 123
uis --json autodial calls 123
uis --json autodial subscribers 123
uis --json autodial employees
uis --json autodial stop-list
uis --dry-run autodial create --payload-file ./campaign.json
uis --json auto-call-campaigns list --limit 10
```

Private reads require a bound cabinet session with supervisor access. `list` requires a JSON file with `begin_date` / `end_date` in `YYYY-MM-DD HH:MM:SS` and optional `statuses` / `search_text`; `export` takes `campaign_ids` / `numa_statuses`, and `validate` takes `numbers` containing `numa`, `name`, `comment`, `origin`. Use `--payload-file` for these inputs (maximum 1 MiB, exactly one object). JSON export returns provider data in the normal envelope; it does not follow download URLs. Campaign get includes its contacts, schedule, attempts, caller-ID and playlist settings. No separate day-of-week or scenario binding contract is claimed.

Private `create`, `update ID`, `status ID` and `listen` produce redacted dry-run plans; actual dispatch fails with exit 7 even with `--force`. Delete fails explicitly because its safe contract remains unverified. `autodial import --input-file contacts.csv` parses a file through the cabinet without saving a campaign; optional `--payload-file` supplies `old_numbers`. The file is bounded to 10 MiB and passed to the provider unchanged; CSV and XLSX parsing were verified live. `autodial exclude|include --payload-file` supports confirmed, single-dispatch stop-list writes with the provider's HTTP 204 response and a read-back assertion; both operations were verified live and the original set restored. Official outgoing methods accept `--source auto|private|dataapi`: auto prefers an existing bound cabinet session, private sends cabinet JWT to RPC without an API key, and dataapi explicitly uses the key. Mutations use confirmation/preflight and one dispatch, but were tested only as plans/refusals. Setting `planned` or `in_process` may start calls; audio preview also calls a number. No campaign write or outbound call was performed during validation. Stop-list add/remove used only a synthetic fixture and cleanup was verified. Supervisor reads and keyless cabinet RPC listing were verified with the existing cabinet JWT on mini; no Data API key was used. See the inventory for the remaining write/cleanup blockers.
