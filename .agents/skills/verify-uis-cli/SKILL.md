---
name: verify-uis-cli
description: "Verify UIS CLI behavior, staff diagnostics and mutation guards before delivery."
---

# Verify UIS CLI

Drive the built executable and keep proof outside the checkout. Read the
[feature map](features/README.md), select the changed feature, and cover each of
its entry points. A green unit suite alone is not a user-path proof.

## Launch

Use this project-local copy in the complete source checkout. A standalone installed
UIS skill bundle has no full build tree; return to the source checkout first.
From the repository root, with Go 1.25+, a C compiler/CGO and Python 3.9+:

```sh
VERIFY_DRIVER="$PWD/.agents/skills/verify-uis-cli/scripts/verify.py"
VERIFY_RUN="$(python3 "$VERIFY_DRIVER" launch)"
python3 "$VERIFY_DRIVER" doctor --run-dir "$VERIFY_RUN"
```

Launch runs the repo's `go build ./cmd/uis` into a new private scratch directory,
records source/binary hashes, and creates a synthetic bound profile. There is no
server, database migration or seed dependency for CLI readiness. Doctor requires
the same source/binary and usable `--version`/`staff --help`. Each drive starts
short-lived child processes; no shared tmux session or installed binary is used.
The default run directory is under `~/.local/state/uis/verification/`, mode 0700.
`launch --evidence-root /tmp/uis-verification` selects another external parent.

## Doctor

Run `doctor` above whenever behavior looks wrong. A cleaned run or changed Go
source requires a new launch. Doctor proves local readiness, **not** provider auth.
For live readiness, the staff-read drive must complete authenticated cabinet GETs;
local `auth status`/`account doctor` only report presence/binding. Expired sessions
fail; refresh separately through the existing authentication workflow, never as an
implicit verification step.

## Drive

```sh
python3 "$VERIFY_DRIVER" drive --run-dir "$VERIFY_RUN" --feature staff-guards
```

This drives actual `staff employees|softphone|sip-lines update` commands using
synthetic patches, first with `--dry-run`, then without `--force`. It expects local
plans and confirmation-required exit 10. It observes a working loopback HTTP proxy,
a credential-helper marker, scratch file hashes and Git refs before/after. The
proxy covers the configured Go HTTP transport, not every possible OS network API.

For live staff reads, select an already authorized profile explicitly. The existing
production harness uses `UIS_E2E_CONFIG` (absolute TOML path) and
`UIS_E2E_ACCOUNT` (account alias); use the same inputs here:

```sh
python3 "$VERIFY_DRIVER" drive --run-dir "$VERIFY_RUN" --feature staff-read \
  --config "${UIS_E2E_CONFIG:?set the existing absolute TOML path}" \
  --account "${UIS_E2E_ACCOUNT:?set the authorized alias}"
```

The helper copies only that bound session into private scratch, strips credential
helpers from the staged profile, runs GET-only commands, and removes the copy in
`finally`. Original config/session hashes must remain unchanged. It compares one
registered and one unregistered employee through card, groups, softphone and line
views. Missing either fixture is a failure, not a skipped comparison. It never
logs in, resets registration, rotates credentials, buys features or writes UIS.

The other mapped features reuse the repo's production E2E harness; follow their
recipes and explicit preconditions. Live mutation verification requires a precise
separate authorization and fixture. Dry-run/confirmation proof does not prove
provider PUT acceptance or read-after-write behavior.

## Evidence

`$VERIFY_RUN/evidence/` retains build output, doctor JSON, command/exit JSONL and
feature result JSON. Every proof names its boundary: real CLI local guards, live
GETs, or an explicitly authorized live lifecycle. Staff live responses stay in
memory; evidence records counts, comparison results and command shapes without
names, contact data, SIP addresses, passwords, tokens or raw provider bodies.

Record an unreachable entry point and its missing precondition as unverified.
For writes, prove the action and a fresh stored-state read, including unchanged
ownership/settings and final residue; a successful acknowledgement is insufficient.
Local protocol tests may stand in for UIS only at the existing HTTP adapter
boundary and must be labelled as such. Keep those results separate from live proof.

## Cleanup

After every successful or failed drive:

```sh
python3 "$VERIFY_DRIVER" cleanup --run-dir "$VERIFY_RUN"
test -f "$VERIFY_RUN/evidence/cleanup.json"
test -f "$VERIFY_RUN/evidence/commands.jsonl"
test ! -e "$VERIFY_RUN/scratch"
```

Cleanup removes only this run's scratch state and copied credentials; evidence and
run metadata survive. The loopback observer shuts down in `finally`; child commands
have bounded timeouts. Never kill processes by name, remove the original session,
or delete an interrupted live-write journal. Concurrent runs have distinct state,
binaries and proxy ports. Concurrent live writes against a shared UIS fixture are
not isolated: refuse them and reconcile the existing journal first.

## Helpers

`python3 .agents/skills/verify-uis-cli/scripts/verify.py --help` documents
`launch`, `doctor`, `drive`, `cleanup`. The [helper](scripts/verify.py) is executable. This project-local
skill is also linked from the root UIS skill, so agents can discover it without
relying on a session's cached skill catalog. Keep its map current with
`/maintain-verification-skill` when commands or proof contracts change.
