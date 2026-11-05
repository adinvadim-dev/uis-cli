# Employees: source-observed private cabinet contracts

Inspected 2026-11-05. This report derives wire contracts from published first-party frontend JavaScript downloaded by the parent investigation. It contains no account response bodies, credentials, SIP addresses or passwords. No write was performed. These are observed client contracts, not a supported public API guarantee. Live compatibility evidence is recorded separately by the parent; source inspection alone does not prove entitlement or accepted partial-update semantics.

## Employee resource and transport

Shell module **4228** defines backend base `https://uis-prod-msk-comagic-api.uiscom.ru/`, `f="common"`, `j="employee-staff"`, `te=common/employee-staff`, export `NE=te`. Its `employees` UI resource aliases to this same backend resource through `resources:{[B]:{name:te}}`. Module **4063** selects the employee resource: ordinary employee editing uses `NE`; the single-employees feature uses the alias `employees`; franchisor branch editing uses `agent/common/employee-staff`. The CLI should support the established ordinary account surface without extending account rights to branch editing. [S1]

The generic provider in chunk **94369** exports `oD=Mo`. Its relevant expressions are:

```text
getOne(resource,{id}) -> GET `${nr(config,resource)}/${String(id)}`
create(resource,{data}) -> POST nr(config,resource), data
update(resource,{id,previousData,data}) -> PUT `${nr(config,resource)}/${id}`,
                                          {...previousData,...data}
```

All use `withCredentials:true`. List requests are GET with direct filter keys; pagination becomes `_start`/`_end` only when metadata enables `remotePagination`, and sort becomes `_sort`/`_order` only when `remoteSort` is enabled. The provider does not use a universal `limit` query parameter. It resolves resource aliases against its base URL before dispatch. Cookies/CSRF or principal verification must follow live session evidence; credentialed browser code does not establish that arbitrary writes are CSRF-exempt. [S2]

Employee edit module **24463** calls `useUpdate` with:

```text
{id:Number(form.id), data:{...transform(form,{mode:"edit",isFranchisor}), id:Number(form.id)}}
```

The `id` therefore appears in both path and body. Avatar updates are separate writes to `common/employee-photo`; two-factor changes are another handler. These do not belong implicitly in a basic employee-edit command. [S1]

## Employee DTO transform and a narrow edit boundary

Transform module **27503**, export `k`, removes `avatarData`, `twoFactorAuth`, `app_role` and `id` from the form, shallow-copies the remainder and performs these transformations:

- Top-level `phone` is normalized when nonempty, otherwise `null`.
- `va_settings` is cloned. If nonempty `phones` exist, normalize priorities and filter the phone collection; default a missing `forward_rule` to `by_priority`.
- When `va_settings.is_active` is false, set `phones=null`.
- If `sip_line_number_capacity_permission.is_update` is true, also set `is_allowed=true` and `is_select=true`.
- In edit mode, remove an unchanged masked `token`; normalize `data_visibility_site_ids`, `data_visibility_group_ids` and `data_visibility_employee_ids` according to the selected app role; remove or normalize agent visibility by franchisor mode; restrict admin-role fields according to vendor policy.

Phone normalization preserves existing phone objects, while synthetic IDs containing `phone` are reduced to `phone,is_active,forwarding_timeout,priority,channels_count,virtual_phone_id,virtual_phone_usage_rule,phone_type`. In edit mode, populated phone entries are prioritized before empty entries; retain only SIP entries or nonempty phone values. This transform proves full-form semantics and substantial unrelated side effects. Do not copy it blindly into a patch command or send the raw read response back. [S1]

Module **77994** proves basic form fields: `last_name` (required), `first_name`, optional `description` (max 500), `email`, `phone`, `staff_group_ids` (multiple) and `main_staff_group_id`. It also contains account-login/password and role/security fields, which are outside the requested ordinary edit scope. A narrow initial employee write allowlist can safely be limited to explicitly requested basic non-secret fields such as `first_name`, `last_name`, `patronymic`, `description`, `email`, plus separately modelled membership fields. Do not imply that the provider accepts an arbitrary partial body: establish or conservatively preserve required current writable fields and verify every requested field through a fresh exact-ID read. [S1]

## Softphone settings are nested employee settings

Module **87964** renders a card titled `Настройки софтфона`, with `B=e=>va_settings.${e}` and three checkbox fields:

| Private DTO field | Observed UI behavior |
| --- | --- |
| `va_settings.is_need_hide_numbers` | Hide customer numbers from the employee in the softphone. |
| `va_settings.is_click_to_call_auto_answer_enabled` | Enable automatic answer for click-to-call; default false. |
| `va_settings.is_out_calls_auto_answer_enabled` | Enable automatic answer for outgoing campaigns; default false. |

These save through the employee edit resource/PUT above; there is no separate observed softphone REST resource in these modules. A dedicated CLI command should validate precisely these booleans and merge them into the existing nested settings without resetting `is_active`, phones, caller-ID permissions, recording, coaching, licenses or other fields. Outgoing auto-answer affects behavior on later calls; reading settings does not initiate a call. The UI also exposes `is_need_out_call_rating`, but outside the softphone card, and recording/coaching fields; they need not expand this initial command. [S1]

## Groups and membership

Module **4228** defines `sI=common/employee-group`. Module **77994** renders `staff_group_ids` as a multiple department selector and `main_staff_group_id` as the primary department, with a fallback/transfer explanation. Ordinary list filtering in module **35987** also uses `staff_group_ids` and the common group resource. The branching-account version can use `agent/common/employee-group`; this is a distinct rights surface. [S1]

This track establishes group reads and membership fields on employee editing. It does **not** establish a current group-create/update/delete UI handler or group DTO. The fact that a generic provider supports PUT/POST/DELETE is insufficient to advertise group CRUD. Group entity reads and employee membership can be covered without inventing group-management writes. Public `groups` versus private `staff_group_ids` must retain provenance; name equality alone does not prove the same identifier namespace.

## SIP-line resource and editable fields

PBX constants module **72255** in chunk **27386** defines:

```text
baseUrl = https://uis-prod-msk-comagic-api.uiscom.ru
R = employee-sip-line
pe = /pbx/employee-sip-line
ge = /pbx/employee-sip-line/replace
resource map [R]: {name:pe}
exports oE=R, cj=pe, WE=ge
```

The shell has the same mapping: module **4228** aliases `common/employee-sip-line` to `pbx/employee-sip-line`. These are frontend resource aliases, not evidence of two interchangeable backend endpoints. [S1, S4]

The SIP modal's main parameters in PBX module **89003** are `state`, `is_multiterminal`, `channels_count`, and `employee_id`. Channel count is bounded by UI min 1/max 2. `is_multiterminal` is shown only with the account component. Shared contracts in chunk **81060** define:

```text
SipEmployeeState.ACTIVE   = "active"
SipEmployeeState.INACTIVE = "manual_lock"
SipEmployeeState.DISABLED = "limit_log"
```

The active toggle maps true to `active` and false to `manual_lock`; the literal `inactive` is not the observed wire value. `limit_log` is a provider/limit state and should not be offered as an ordinary user-settable enum merely because it is returned. A narrow non-secret initial line-edit allowlist is `state` (`active|manual_lock`), `channels_count` (1..2) and `is_multiterminal` (boolean), subject to live provider permissions. [S3, S5]

## SIP Save and reassignment are different operations

PBX Save module **32341** uses `useUpdate(m.oE,{data:form,id:record.id})` for ordinary editing, producing PUT `/pbx/employee-sip-line/{id}`. Before dispatch it sets `email` from the selected employee, normalizes several caller-ID nested objects, and clears fields whose rule is no longer selected. When `form.is_change_employee_id` is true, it first calls `useCreate(m.WE,{data:{new_employee_id:form.employee_id,sip_line_id:form.id}})`, then performs the PUT only after that POST succeeds. Thus cabinet reassignment is a **two-write** POST `/pbx/employee-sip-line/replace` plus PUT, not a simple `employee_id` patch. Avoid exposing reassignment as an ordinary safe scalar edit; implementing it requires partial-state recovery and explicit compound-action confirmation. [S3, S4]

Caller-ID module **32922** exposes `communicationDirection`, `virtual_phone_usage_rule`, `virtual_phone_id` and nested automatic-selection/permission rules. Its UI includes feature/limit checks and purchase links. Ordinary diagnostics or line-state changes must not broaden entitlements or activate paid components. Copy-settings controls can also copy unrelated configuration; leave them out of the narrow edit surface unless separately modelled. [S3]

## Registration state, recent registrations and the misleading refresh icon

Module **89003** reads `sip_line_registration` from the line record. It displays `user_agent`, `ip`, and `start_time`. It labels the table “active registrations” or “last registrations” by checking `is_closed` on the first registration entry. It creates a random UUID solely as a frontend row key; that is not a stable provider registration ID. There is no pagination/cursor or retention promise for this nested table, and it is not evidence of a complete registration history. Redact network addresses and inspect/sanitize user-agent text before including diagnostic output. [S3]

The password card in the same module contains a `Refresh20` icon. Its handler **only** generates a new local 8–10 character password, marks the password field dirty, and sets `is_send_info_to_mail` when an employee email is available. The resulting password is saved by the ordinary PUT. This is password rotation, potentially with email delivery, **not a registration-reset API**. No registration-reset/re-register/disconnect endpoint was established by the inspected source. Do not invent one. Password rotation and config download require a separate secret-safe product boundary and remain explicit deferrals here. [S3]

## Source register

Public bundles inspected locally, with module identifiers/expressions above:

- **S1:** [UIS shell chunk 28188](https://go.uiscom.ru/static/js/28188.6ca386fd.chunk.js), SHA-256 `634c0977850edf9d5ddf671b8cd1c613992b6b13955507501e96c1bdb908ab67`; modules 4228, 4063, 24463, 27503, 77994, 87964, 35987.
- **S2:** [UIS CRUD/provider chunk 94369](https://go.uiscom.ru/static/js/94369.d8a08ccf.chunk.js), SHA-256 `bf1762d3d58ef48d08cd48a5a10ad700554c61e2a8017e3d0c95114182ba8b25`; exported `oD=Mo`, `getOne`, `getList`, `create`, `update`.
- **S3:** [UIS PBX editor chunk 22718](https://uis-prod-msk-comagic-pbx-frontend.uiscom.ru/static/js/22718.3af87d8b.chunk.js), SHA-256 `ba9873fb4371a614e97a6f08ee2202368e469dfaecd4d41b6cdde5e08859a16a`; modules 32341, 32922, 89003.
- **S4:** [UIS PBX constants chunk 27386](https://uis-prod-msk-comagic-pbx-frontend.uiscom.ru/static/js/27386.9f230987.chunk.js), SHA-256 `701676b51dc894d5182871479d5fa3d69e454117e142dca48e082fafb1efb467`; module 72255.
- **S5:** [UIS PBX shared contracts chunk 81060](https://uis-prod-msk-comagic-pbx-frontend.uiscom.ru/static/js/81060.b70dfd7f.chunk.js), SHA-256 `8b440f34c7630c9542b07324fa16c3a025d39a4758222f74302b1864f98cd03a`; exported `SipEmployeeState`.
