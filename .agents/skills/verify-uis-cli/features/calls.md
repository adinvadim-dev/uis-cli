# Calls and discovery

Users search calls, inspect a selected timeline/legs and discover the supported report/filter vocabulary.

## Sub-features

- `calls-search`: date/ID/lost/transfer filtering.
- `calls-detail`: private details and explicit degraded fallback.
- `calls-discovery`: reports, filter catalog, saved and rapid filters.

## How to get to it (user POV)

- `uis calls search`, `uis calls show ID`, `uis calls legs ID`.
- `uis reports list`, `uis filters list|show|rapid`.
- Generated `uis account get` and low-level `uis call get.account` verify official API credential acceptance.

## Driving it with production E2E

Preconditions: existing authorized profile/session, Data API credentials through its configured helper, at least one call in the last seven days and a support ticket (the existing core suite also covers support).

- **Run existing real-binary suite.** With all `UIS_E2E_*WRITE*`, `UIS_E2E_LOGIN` and resume opt-ins unset, run `UIS_E2E_PRODUCTION=1 go test -tags production ./e2e -run '^TestProduction$' -count=1 -v -timeout 20m > "$VERIFY_RUN/evidence/core-suite.log" 2>&1`. Export explicit `UIS_E2E_CONFIG` and `UIS_E2E_ACCOUNT` first.
- **Observe.** Require exit 0 and passing live call/discovery subtests. The suite derives the real selected ID and checks the resulting record, not only a successful list response. Record omitted optional paths as unverified; inspect the maintained coverage in `docs/PRODUCTION_E2E.md`.
- **Retain.** The Go harness cleans its own private copies; the log under evidence survives the skill cleanup. It suppresses raw provider content.

## Gotchas

- The full existing suite needs both private and Data API surfaces. A missing credential or fixture is a failed precondition, not proof of the remaining surface.
- Private communication IDs and classic call-session IDs are not interchangeable. Private employee/scenario filters use names; numeric filters need Data API.
- Ordinary commands do not refresh sessions automatically. Login/refresh may involve paid CAPTCHA and are separate from these reads.

## Operator analysis extension

- Entry points: `calls search --source private --analysis`, the same with `--all`
  and explicit dates/page cap, and `scripts/operator_compare.py --help` plus the
  [cross-CLI recipe](../../../../docs/OPERATOR_COMPARISON.md).
- Local protocol proof: `go test ./internal/cli -run 'TestPrivateAnalysisPagination|TestAnalysisFlagsValidateBeforeCredentials'`; Python real-helper fixtures:
  `python3 -m unittest discover -s tests -p test_operator_compare.py -v`.
- Drive the launched binary against the existing bound read-only profile with
  `--analysis --limit 3`, then an exact known employee with `--all --limit 1000`.
  Store raw responses in protected external files; record only field presence,
  source enum/counts, paging coverage and hashes in shareable evidence. Verify
  the offline helper against actual saved UIS and Medivey snapshots, naming any
  missing Medivey access/identity/timezone prerequisite. No mutation or grant.
- All-page results are bounded live observations, not historical snapshots.
  Numeric employee ID filtering is local to the helper, not a private API claim.
