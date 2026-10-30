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

## Coverage and limits

| Area | Verification |
| --- | --- |
| Data API | Live credential acceptance through generated `account get` and low-level `call get.account` |
| Calls | Live discovery, ID filtering, private details without accepting fallback, legs, lost/transfer filters |
| Discovery | Live reports, report filters, saved filters and rapid filters; entitlements required |
| Private support | Live topics, contacts, ticket list and ticket/thread detail |
| Archive | Full sync, ready/fresh/not-degraded state, local list/show and FTS retrieval of a discovered ticket |
| Accounts/auth/capability | Local inspection, isolated logout, optional fresh private login |
| Domain mutations | Local dry-run plans for comment, tag add/remove and ticket create/reply/close/reopen; confirmation and invalid-input exit codes |
| Embedded Data API surface | Every spec method through generated and low-level dry-run, checking RPC method identity; no live dispatch |

This is a production read suite with local mutation-plan coverage. A green run does **not** prove live mutations, every generated resource command against production, every optional filter or every Data API method works. Existing generated-command tests cover routing and typed required parameters locally. Live writes need separately authorized fixtures and a lifecycle with read-after-write assertions; never run arbitrary generated mutations against customer data.

The runner pins Data API host/version to UIS production, disables retries, uses noninteractive commands and validates one `uis.cli/v1` JSON envelope plus the process exit status. It builds a temporary binary, copies only the selected profile and (unless fresh login is requested) session into a private temporary state directory, and creates a new archive there. Logout removes only that copied session. The original profile, session and archive are preserved. Offline archive checks remove the token, replace the helper with a nonexistent executable and disable the Data API route.

Outputs are captured in memory; failures report fixed assertion messages, exit codes and machine error categories. Raw CLI output, credentials, phone numbers and support contents are not printed. Go cleans the temporary state on normal completion; a killed process can leave sensitive temporary files, which should be handled like the user's normal session/archive.
