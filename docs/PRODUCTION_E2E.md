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
