# Employees: official UIS surface

Researched 2026-11-05 from first-party public documentation and the repository's embedded specification. No credentials, customer records, SIP addresses or passwords were read or retained. No provider mutation was performed. This report establishes documented capabilities; it does not prove the authenticated cabinet's current endpoint contracts or this account's entitlements.

## Acceptance boundary

The public Data API can read employee cards, employee groups and SIP registration, and can edit employee/group/SIP configuration. It does **not** establish the richer cabinet softphone settings or registration-reset contract. Cabinet reads and source-observed write contracts therefore remain necessary for the requested feature.

Employee availability, SIP registration and billing state are separate dimensions. A working-status label such as available does not prove a registered SIP endpoint. Conversely, registration alone does not prove the number is enabled, unblocked or available in its employee group.

## Documented read/write matrix

| Surface | Official methods | Evidence and limits |
| --- | --- | --- |
| Employee card | `get.employees`; `create.employees`, `update.employees`, `delete.employees` | [Employee list](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/employee/get_employees/), [employee update](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/employee/update_employees/). Identity/name/login/email, status/status_id, call-direction permissions, schedule, recording, coach, group membership, ordered phones, extension and chat-operator properties. Legacy status/calls_available/allowed-call-types fields are deprecated in favor of status_id and directional permissions. The chat `operator` object is not evidence of the cabinet's softphone-user DTO. |
| Groups | `get.group_employees`; `create.group_employees`, `update.group_employees`, `delete.group_employees`; `update.group_employees_numbers` | [Group list](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/employee/get_group_employees/). Membership includes employee IDs/names; phone rows carry separate employee-phone identity, availability and list-order priority. Group extension, queue and simultaneous channels are distinct from an employee's own configuration. Phone-priority update requires the complete ordered phone list, not a sparse patch. Documentation's example uses `phone_number` while its response table says `phone_numbers`; adapters must follow observed payloads, not silently assume either spelling. |
| SIP registration/configuration | `get.sip_lines`; `get.sip_line_virtual_numbers`; `create.sip_lines`, `update.sip_lines`, `delete.sip_lines`; `update.sip_line_password` | [SIP list](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/sip/get_sip_lines/), [SIP update](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/sip/update_sip_lines/). Read fields include employee_id, physical_state, billing_state, employee-phone status, channels, dial time, caller number, active registration IP/time and calling restrictions. Registration values are documented as Russian strings. Update can reassign employee, configure caller number/channels, manual lock and call restrictions. No reset-registration method was found in the embedded public specification. |
| Softphone settings | No dedicated employee softphone method found in the embedded public specification | Public cabinet manuals describe these settings; their private endpoints and field names require authenticated/source inspection. Never route them to unrelated chat-operator fields. |

## Current cabinet manuals: richer UI than the public API

The [SIP numbers manual](https://www.uiscom.ru/academiya/spravochnyj-centr/virtualnaya-ats/sip-nomera/), published 2026-03-19, describes registration indicators, resetting registration, current/latest registrations with user agent/IP/time, multiterminality, enabled state, channels, communication direction, caller-ID rules, softphone caller-ID visibility and reassignment. Creation is license/limit-dependent; the manual describes up to three SIP numbers per employee. It also describes password generation and optional email delivery. These are separate capabilities with operational effects; documentation alone does not authorize invoking them or reveal their private API.

The [current employee manual](https://www.uiscom.ru/academiya/spravochnyj-centr/sotrudniki-i-otdely/sotrudniki/) describes license-based configuration, hiding callers' numbers in the softphone, caller-ID visibility/selection and auto-answer for click-to-call. Enabling a PBX license creates a SIP line. Roles, licenses and API access are adjacent surfaces, not implied scope for editing production rights.

The [legacy employee/group manual](https://www.uiscom.ru/academiya/spravochnyj-centr/sotrudniki/upravlenie-sotrudnikami-i-gruppami/) documents a separate softphone-user tab and group phone availability/priority. Employee state is refreshed by reloading rather than automatically. A configured CRM status import can overwrite manual status changes. The [legacy softphone-user manual](https://www.uiscom.ru/academiya/spravochnyj-centr/starye-versii-softfona/sozdanie-polzovateley-softfona/) describes optional email of setup data, user rights and outgoing caller-ID selection. Treat these as historical UI capabilities, not proof of the current account's exact fields or access model.

## Authentication, pagination and quotas

[Data API conventions](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/) specify HTTPS JSON-RPC POST on the provider's versioned endpoint; current version is 2.0. API access needs an allowed source IP and API-user permissions. Authentication uses a user API key or login/password session (documented lifetime one hour). Agent requests need client user_id. These credentials are distinct from cabinet app/billing/JWT sessions.

List methods support offset/limit, filtering, fields and sorting. Documented defaults are offset 0 and limit 1000; maximum offset 100000 and limit 10000. Metadata includes total_items and minute/day quota information. Successful calls consume weighted points (the current table lists one per operation); quota expansion is a paid account decision. This report neither changes an allowlist nor requests broader rights or quotas.

## Embedded-spec comparison and implementation cautions

`internal/spec/data/api_spec.json` already contains the employee/group/SIP methods above, so adding another generated `get.employees` wrapper alone would not satisfy this task. Its schema stores request parameters but not response fields, registration enums or cabinet settings.

The current SIP-update documentation includes `is_international_allowed`, `finance_limit_international` and `is_zone_allowed`, absent from the embedded snapshot. The employee-update snapshot flattens nested phone/extension/operator fields, duplicates `status` and marks nested members as top-level requirements; it is unsuitable as an authoritative DTO for a new private adapter. Keep this feature's observed private contracts typed and separate; refreshing all generated methods would be a separate change.

Useful safe output is an allowlisted diagnostic projection: stable employee/line/group IDs, statuses, booleans, channels and non-sensitive comparison fields. Suppress SIP usernames/addresses, passwords, tokens and setup-email contents. Registration IP and user-agent data require deliberate handling rather than raw dumps. A registration-reset acknowledgement is not proof of subsequent successful registration; read the current state afresh and state that limitation.

## Remaining evidence required

Discover the authenticated employee list/detail, groups, softphone-user and SIP-line endpoints, including pagination and actual field names; determine the account/principal and session used by each. Observe write contracts from provider frontend source without live trial writes. Establish safe editable fields, preservation of unedited secrets, preflight, send-once dispatch and bounded read-back. Document registration-reset verification separately from ordinary field-update reconciliation, and leave unsupported operations explicit.
