# Autodial

Users inspect outgoing campaigns and catalogs, prepare changes and independently authorize campaign/contact lifecycle actions.

## Sub-features

- `autodial-read`: campaign/status/contact/report/catalog reads and parse-only imports.
- `autodial-routes`: keyless generated/raw routes and safe guard coverage.
- `autodial-write`: separately authorized stop-list and future campaign lifecycle.

## How to get to it (user POV)

- `uis autodial list|get|statuses|calls|subscribers|export`.
- `uis autodial employees|caller-rules|caller-numbers|caller-groups|caller-lists|media|scenarios|schedules|timezone`.
- `uis autodial import|validate|stop-list`, generated `auto-call-*`, and low-level `uis call` routes.
- Create/update/status and transition commands require independent authorization; start/resume/listen can initiate calls or charges.

## Driving it with production E2E

Preconditions: an authorized bound supervisor session and explicit `UIS_E2E_CONFIG`/`UIS_E2E_ACCOUNT`. A campaign fixture is required for dependent detail paths.

- **Run existing read/guard suite.** With all write/login/resume opt-ins unset, run `UIS_E2E_PRODUCTION=1 UIS_E2E_AUTODIAL=1 go test -tags production ./e2e -run '^TestProductionAutodial$' -count=1 -v -timeout 20m > "$VERIFY_RUN/evidence/autodial-suite.log" 2>&1`.
- **Observe.** Require exit 0 and inspect subtest results. Empty campaign lists can skip dependent paths in this existing harness: explicitly report those paths unverified. Read/guard success never certifies a lifecycle write.
- **Live lifecycle.** Leave unverified without the separate opt-ins, durable journal, approved synthetic contacts and accepted cancelled residue in `docs/PRODUCTION_E2E.md`. An old owned journal only proves read-back of that prior resource.

## Gotchas

- `campaigns` in the CLI means advertising; outgoing calling uses `autodial`/`auto-call-*`.
- Empty-draft creation is not supported; valid creation schedules a campaign. Do not create a fixture or activate audio to make a verification run pass.
- The suite's optional write skips and empty-fixture skips must remain visible in the feature report. Physical deletion is not available.
