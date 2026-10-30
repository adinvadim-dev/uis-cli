# Production E2E

`make test-production` builds the current CLI and executes it as child processes against real UIS production APIs. It uses real credentials, not scripted HTTP servers. Ordinary `make test` / `go test ./...` never run it. No GitHub Actions workflow runs it.

## Run

Use an existing non-secret TOML profile with `expected_login` and an external `credential_process` as described in [AUTHENTICATION.md](AUTHENTICATION.md). Supply the Data API token through that helper or an already exported `UIS_TOKEN`. Never put credentials in command arguments, test fixtures, reports or Git.

```sh
UIS_E2E_PRODUCTION=1 \
UIS_E2E_CONFIG="$HOME/.config/uis/config.toml" \
UIS_E2E_ACCOUNT=team \
make test-production
```

The account must already have a private session, a call within the last seven days, and at least one support ticket. Missing fixtures, insufficient permissions, expired credentials, unsupported methods and provider errors fail the relevant test; they are not silently skipped or accepted as success. Data API account reads verify provider acceptance; `auth login --surface dataapi` alone cannot do that.

To exercise fresh private login instead of reusing an existing session:

```sh
UIS_E2E_PRODUCTION=1 \
UIS_E2E_CONFIG="$HOME/.config/uis/config.toml" \
UIS_E2E_ACCOUNT=team \
UIS_E2E_LOGIN=1 \
make test-production
```

Fresh login may create a paid 2Captcha task. It needs portal and captcha fields from the helper. Without `UIS_E2E_LOGIN=1`, the test logs that login/refresh were **not tested**. Refresh is not currently exercised in either mode.

## Live support writes

Only enable this after authorizing actual messages to UIS support. It creates one clearly labeled test ticket, verifies its initial comment, replies once, closes, reopens and finally closes it. Each step is checked with a fresh live ticket/thread read, including exact message content, message identity, absence of duplicates and status. It does not mutate existing tickets, call comments or tags.

Supply real topic, contact, customer-phone and customer-email IDs for the selected account. Use a private, durable directory for the journal, outside the test's temporary state and outside Git. Its parent directory must already exist. Example (replace all angle-bracket placeholders):

```sh
umask 077
mkdir -p "$HOME/.local/state/uis/e2e"
UIS_E2E_PRODUCTION=1 \
UIS_E2E_CONFIG="$HOME/.config/uis/config.toml" \
UIS_E2E_ACCOUNT=team \
UIS_E2E_SUPPORT_WRITE=1 \
UIS_E2E_TOPIC_ID='<topic-id>' \
UIS_E2E_CONTACT_ID='<contact-id>' \
UIS_E2E_PHONE_ID='<customer-phone-id>' \
UIS_E2E_EMAIL_ID='<customer-email-id>' \
UIS_E2E_WRITE_JOURNAL="$HOME/.local/state/uis/e2e/run-$(date -u +%Y%m%dT%H%M%SZ).jsonl" \
make test-production
```

Without `UIS_E2E_SUPPORT_WRITE=1`, this scenario explicitly reports a skip. With opt-in enabled, missing IDs or a missing/occupied journal path fail before writes. The journal is created exclusively with mode 0600, appended and flushed before each dispatch and retained afterwards. Each JSONL record contains the account, unique marker, known ticket ID and stage, not credentials or customer messages. Completed records survive a torn final append. A sibling `.lock` prevents concurrent runs; a killed process leaves that lock for manual reconciliation. Never delete an interrupted run's journal or immediately rerun with another path: doing so can create another real ticket.

There is no automatic write retry or failure cleanup. On failure or `outcome_unknown`, stop and reconcile the known ticket through live reads; when create did not return an ID, locate the unique journal marker in the live ticket list before any further write. A two-stage create failure preserves a returned ticket ID when available. Close only that verified test ticket manually if needed, without resending an ambiguous close. A successful run retains a final record with `stage: complete` and leaves the ticket closed; the ticket and messages remain in UIS history.

Two narrow recovery cases are supported with `UIS_E2E_SUPPORT_RESUME=1`, the **same** journal and account. Stage `create_returned` continues before any reply/status dispatch: it never calls create and first verifies the exact marker/body, initial comment and open status. Stage `reopened_verified_dispatch` only reads to reconcile the previously dispatched reopen: if the ticket is open and both exact test messages are present once, it sends only the final close, never another reopen. Malformed journals, partial records, wrong accounts and all other stages are refused. If a process died, confirm it is no longer running before manually removing its stale `.lock`. Other stages need manual reconciliation. Live creation and reopening have been observed to store `waiting_operator`; the test accepts that open state as well as `created`.

## Coverage and limits

| Area | Verification |
| --- | --- |
| Data API | Live credential acceptance through generated `account get` and low-level `call get.account` |
| Calls | Live discovery, ID filtering, private details without accepting fallback, legs, lost/transfer filters |
| Discovery | Live reports, report filters, saved filters and rapid filters; entitlements required |
| Private support | Live topics, contacts, ticket list and ticket/thread detail |
| Support writes (separate opt-in) | One test ticket: create, reply, close, reopen, final close; live read-after-write assertions and durable journal |
| Archive | Full sync, ready/fresh/not-degraded state, local list/show and FTS retrieval of a discovered ticket |
| Accounts/auth/capability | Local inspection, isolated logout, optional fresh private login |
| Domain mutations | Local dry-run plans for comment, tag add/remove and ticket create/reply/close/reopen; confirmation and invalid-input exit codes |
| Embedded Data API surface | Every spec method through generated and low-level dry-run, checking RPC method identity; no live dispatch |

By default this is a production read suite with local mutation-plan coverage. A green run proves the live support lifecycle only when its separate opt-in scenario ran and passed. It does **not** prove live call annotations/tags, every generated resource command against production, every optional filter or every Data API method works. Existing generated-command tests cover routing and typed required parameters locally. Never run arbitrary generated mutations against customer data.

The runner pins Data API host/version to UIS production, disables retries, uses noninteractive commands and validates one `uis.cli/v1` JSON envelope plus the process exit status. It builds a temporary binary, copies only the selected profile and (unless fresh login is requested) session into a private temporary state directory, and creates a new archive there. Logout removes only that copied session. The original profile, session and archive are preserved. Offline archive checks remove the token, replace the helper with a nonexistent executable and disable the Data API route.

Outputs are captured in memory; failures report fixed assertion messages, exit codes and machine error categories. Raw CLI output, credentials, phone numbers and support contents are not printed. Go cleans the temporary state on normal completion; a killed process can leave sensitive temporary files, which should be handled like the user's normal session/archive.

## Autodial supervisor reads

Run only the new read probe with an already refreshed, bound session:

```sh
UIS_E2E_PRODUCTION=1 UIS_E2E_AUTODIAL=1 \
UIS_E2E_CONFIG=/absolute/config.toml UIS_E2E_ACCOUNT=team \
go test -tags production ./e2e -run '^TestProductionAutodial$' -count=1 -v
```

The suite isolates config/session copies and exercises all 17 private commands. Eleven read/parse scenarios dispatch to production: statuses, employees, stop-list, list, get, calls, subscribers, export, validate and import. It checks catalog transitions, campaign IDs/contacts/attempts, statistics fields, the cabinet export columns (`numa`, `campaign_name`, `status_name`) when rows exist, and rejection of a deliberately invalid phone string. Import fixtures use only a synthetic phone string, never saved into a campaign.

`UIS_E2E_AUTODIAL_FROM` / `UIS_E2E_AUTODIAL_TILL` override the default yesterday/today cabinet date window (`YYYY-MM-DD HH:MM:SS`). List requests use a 60-second timeout. The first returned campaign is used only for reads; `UIS_E2E_AUTODIAL_CAMPAIGN_ID` can select an explicit existing fixture. If list is empty with no explicit fixture, the four dependent reads are skipped with an explicit unverified notice. Export uses the cabinet UI's `unsuccessfully_processed` and `not_processed` statuses. An empty result logs that nonempty export remains unverified.

Four gated private writes (create/update/status/listen) are checked as dry-run plans and forced capability refusals. Delete must refuse both dry-run and forced execution. CSV and XLSX import are tested live as a parse-only operation without campaign persistence. These checks remove usable credentials/session state independently of the CLI guard, so a future regression cannot send a write to an arbitrary production ID. Every official method is tested through both generated and low-level routes; the three official mutations additionally must return confirmation_required before auth/network. They are never sent with force. Official live listing is required to pass through both generated/raw commands with auto/private sources: the API-key helper is unusable and UIS_TOKEN empty, but the staged cabinet session remains available. Failure is not skipped. Supervisor JWT success does not imply Data API-key availability.

The suite never creates campaigns, changes contacts/schedules, starts/resumes calls, previews audio or refreshes login. A production write lifecycle remains blocked until a non-starting create and cleanup contract are established. Guard coverage is not successful mutation proof. Outputs contain test names and sanitized error categories only; customer rows, phone numbers, credentials and provider error bodies are suppressed.

The earlier 21 PASS / one SKIP result exposed missing private RPC routing. The keyless route is now a required production assertion; updated results are recorded in the coverage inventory. This proves every new command's currently supported behavior, including explicit refusals; it does not prove disabled mutations work against the provider.

## Exclusion writes (separate opt-in)

With the ordinary autodial read opt-ins, set `UIS_E2E_AUTODIAL_EXCLUSION_WRITE=1` and `UIS_E2E_AUTODIAL_EXCLUSION_JOURNAL=/absolute/new-file.json` to authorize one synthetic stop-list add/remove. The suite refuses a pre-existing fixture, exclusively creates a durable 0600 journal, fsyncs dispatch/recovery stages, adds once through the CLI, verifies presence, and removes only its own fixture in deferred cleanup. It then checks the initial exclusion set is restored. Never overwrite an existing journal to rerun: inspect its final stage and reconcile an interrupted operation first. This is independent of campaign creation and cannot initiate calls.

Verified on mini: 24 subtests PASS, zero SKIP with exclusion-write opt-in. CSV/XLSX parse, POST/DELETE 204 handling, read-backs and cleanup_verified passed. Campaign lifecycle mutation success is not implied by this result.

Recovery tests also run without the production build tag or network. A timed-out add followed only by absent reads retains add_unresolved: a late server commit must never be recorded as cleanup_verified. An acknowledged or positively observed add can be compensated and verified. The late-commit regression fails without that fence and passes with it.

If persisting remove_dispatch fails, cleanup does not send DELETE: the preceding unresolved/acknowledged-add journal state is retained for manual recovery. A separate offline regression verifies this failure never bypasses durable dispatch recording.

## Empty campaign create probe: unresolved provider response

After explicit authorization to retain one empty cancelled test campaign, one direct authenticated provider probe requested an empty draft (no recipients/employees, future date, audio disabled). It returned HTTP 500 with root value_error.exception and no ID. Three follow-up exact-marker list reads found no resource; the durable journal remains create_unresolved. The POST was not repeated. This empty DTO is not established as a valid full campaign create contract, so the result does not prove that all create requests fail.

Campaign update, contact and status write E2E remain blocked by the missing owned fixture. Never turn an absent read after a 5xx into successful cleanup or an automatic retry. Reconcile the first dispatch/provider contract before any new creation; preserve the one-resource authorization. The production read/import/exclusion suite remains separate from this unsuccessful create probe.
