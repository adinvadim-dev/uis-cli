# Forwardable recording links

A request for a specific call and an approved recipient is required. Listen using
`recording download ID` and check the selected content and intended audience under
existing disclosure rules. Do not treat CLI access or this skill as permission.
If audience or content approval is missing, resolve it before disclosure.

```sh
uis --json recording link ID --leg 1 --audience-approved
```

The command verifies the call-detail ID and report-row ID, accepts only the
existing HTTPS media host/path allowlist, and fully downloads one selected leg
without cabinet credentials. It emits the URL only after nonempty audio succeeds,
alongside bytes, SHA-256, content type and verification time. Compare the hash with
the selected `recording download` file before sending. Multiple legs require an
explicit selection; the command never emits all links. Dry-run discloses nothing.

The URL itself grants access to that recording. Forward only to the agreed
recipient, with link previews disabled where possible; never publish it, include
it in logs/PRs, or batch-export recordings. Do not forward session tokens or raw
CLI envelopes. Tell the recipient:

> Ссылка доступна до удаления записи в UIS по сроку хранения вашего тарифа.
> Точная дата окончания неизвестна; скачайте запись сейчас. Ссылку можно
> передать только согласованному получателю: доступ имеет любой её обладатель.

No CLI-controlled short TTL or individual revocation exists for this provider
link. If the task requires either, do not issue it; report that limitation.
`expires_at: null` means unknown, never unlimited. Verification is point-in-time.
On 404/410 (exit 5), retention/deletion may have removed audio; 401/403 (exit 4)
means the media cannot be accessed. Re-resolve the same call once to check for a
changed link; never alter tokens, substitute another call, or promise renewal of
a deleted recording. Other provider/transport/schema errors disclose no URL.

## Mechanism and evidence

UIS documents record-specific links for integration and links lasting for the
recording retention period:
[online downloads](https://www.uiscom.ru/academiya/spravochnyj-centr/otchety/skachivanie-zapisey-zvonkov-v-rezhime-onlayn/),
[CRM recording links](https://www.uiscom.ru/academiya/spravochnyj-centr/integratsiya-s-samopisnymi-crm/zagruzka-zapisey-razgovorov-v-crm/).
The cabinet report's observed `/system/media/talk/ID/TOKEN/` links already serve
credential-free audio in the existing downloader. Their exact individual expiry
is not exposed by that contract; do not infer a tariff or manufacture a date.
Using the existing provider capability avoids uploading personal audio to new
storage or creating a new publicly accessible proxy. This is a read-only local
CLI/skill addition; no deployment, account grants or business-data writes.
