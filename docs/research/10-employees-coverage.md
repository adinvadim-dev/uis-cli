# Employee cabinet coverage

Research: 2026-11-05. Private contracts are observations, not provider guarantees.
The existing `uis employees` and `uis sip-lines` generated Data API commands remain
separate from the new `uis staff` cabinet commands. No new API key is needed.

## Evidence and boundaries

Authenticated Chrome inspection reached `/common/staff/employee-staff`, an employee
editor (including its softphone card), and `/pbx/employee-sip-line`. No Save,
registration reset, credential regeneration, assignment, provisioning or other
provider mutation was performed. Local account-bound CLI session reads proved
`GET /common/employee-staff` returns an unpaginated array, its `/{id}` returns the
full editor DTO, and `GET /pbx/employee-sip-line` returns registration state and
registration rows. `?limit=1` on employee list returned 500; pagination parameters
are deliberately omitted. Counts, names, addresses and credentials are not fixtures.

Public source evidence: Go shell `28188.6ca386fd.chunk.js` modules 24463, 27503,
87964, 4228; generic REST provider `94369.d8a08ccf.chunk.js`; PBX frontend
`22718.3af87d8b.chunk.js` modules 32341/89003/32922 and constants 72255.
See [private contract research](09-employees-private-contracts.md),
[official surface](07-employees-official.md) and
[action/archive analysis](08-employees-actions-archive.md).

| Scenario / source | Command | Coverage / safety |
| --- | --- | --- |
| Private employee card, availability and SIP summary | `staff employees list|get ID` | Explicit projection, current observation timestamp; employee availability and SIP registration are separate |
| Private group catalog and membership | `staff groups list|get ID`, `staff employees groups ID` | Exact IDs, membership resolved against catalog; groups are current cabinet staff groups, not assumed equal to Data API legacy groups |
| Private employee editor | `staff employees update ID --payload-file FILE` | Basic contact/name, schedule and group membership patch; fresh writable DTO preserved, one PUT and effect read-back |
| Employee softphone card in `va_settings` | `staff softphone get|update ID` | Hide customer numbers, click-to-call auto-answer, other outbound auto-answer; booleans only, active VA required |
| Private SIP registration and last/current registrations | `staff sip-lines list --employee-id ID`, `get ID` | Registration boolean distinct from line lock state; safe UA/time/closed projection; IP and credentials omitted |
| Private SIP line settings | `staff sip-lines update ID` | State (`active`/`manual_lock`), channels (1–2), multiterminality; no credential or assignment writes |

All writes require task authorization and confirmation/`--force`; dry-run validates
locally and does not read config, sessions, credentials or network. Plans expose
field names only. Writes are never retried. A successful HTTP reply alone does not
verify a mutation: a fresh target read must match every requested field. Ambiguous
or mismatched effects return exit 9; re-read before deciding whether to retry.
Live write acceptance is not claimed: this task authorizes implementation and
read-only verification, not a specific production employee change.

## Explicit gaps

The current SIP editor contains a password refresh icon which generates a new
password locally for a later Save. It is **not** reset registration. Although UIS
help documents reset registration, a corresponding current private request has
not been established; no guessed reset command is offered. SIP registration rows
are the cabinet snapshot, not complete event history. No continuous health or
historical completeness guarantee exists.

Credential generation/reveal/email, employee creation/deletion, roles, API rights,
licenses, caller-ID permissions, number provisioning, SIP reassignment and group
CRUD are outside this operational diagnostic/change boundary. SIP reassignment
uses a separate replace POST followed by PUT; it is intentionally deferred rather
than approximated by a single employee_id change. Group membership can be edited
on an existing employee. Caller-ID options are visible in the UI but their writes
remain deferred; the three softphone switches are covered.

No archive is added: these commands explicitly fetch current operational state.
Employee/group revisions, long-term SIP event history and offline exports are
named deferrals. Raw DTOs remain inside the adapter and are never emitted or saved
by the feature. Output omits SIP login/URI/password/server, IPs, API/session tokens,
contact addresses and arbitrary private DTO fields.

Write compatibility limit: the frontend submits a full form; the server's minimal
required DTO and omission semantics have not been verified by a live write. The
CLI excludes auth/credential fields and checks unchanged rights/configuration in
read-back. A rejection or unrelated change is a blocker, not permission to resend
a wider payload. No version/ETag concurrency contract was observed; the initial
GET minimizes stale state but cannot prevent a simultaneous cabinet edit.

Read-only command verification on 2026-11-05: employee list (480), group list
(2223), group detail, employee detail/groups/softphone and exact employee-filtered
SIP list/detail all returned successful CLI envelopes. Two selected employees had
registered and unregistered SIP lines respectively. The detail card's own summary
registration/status fields can be null even when the line list has a current
boolean; employee detail therefore includes linked `sip_lines` from the line API.
Counts are observations, not fixed account fixtures or a snapshot guarantee.
