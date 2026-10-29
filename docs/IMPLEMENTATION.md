# Implementation status

This document records implementation limits without changing the frozen specs.

## Available

Data API search, report legs, call comments/tags, filter/report discovery and all existing generated commands share one send-once mutation boundary. JSON-RPC response IDs are verified. Transient retries apply only to read method namespaces. Provider bodies and credential-helper stderr are suppressed from errors. JSON output uses `uis.cli/v1`; legacy exit codes 10/11/12 map to auth 3, rate 8, provider 6 (permission 4 and ambiguity 9 are distinct).

Non-secret TOML profiles, lazy strict credential_process, environment token precedence, safe account names and per-account state directories are implemented. A reusable session promotion boundary verifies a provider-derived principal before atomic replacement. It preserves the previous session on mismatch or error. App and go tokens remain separate.

Private support adapters implement GET ticket/thread/topic/contact reads, multipart ticket plus initial comment, multipart reply and PUT close/reopen with status reconciliation. Scripted tests exercise wire contracts; no production readiness is inferred. A two-stage create error retains known provider ticket identity. Mutation preflight requires a verified principal callback and cannot be bypassed by force. The CLI has no verified production callback yet.

The authorized fallback archive uses local SQLite / FTS4 (CGO), staying on Go 1.25 instead of requiring crawlkit's Go 1.27. `support sync` writes pages and cursors transactionally, revisits all tickets/threads and advances final freshness only on complete success. Failed sync retains older freshness and marks degradation; partial records remain queryable. It restarts discovery on the next run rather than trusting undocumented durable offsets. No attachment bytes or bearer download locators are preserved. Live reads do not populate the archive.

## capability_unavailable and follow-up

- **Private login / refresh:** legacy login and SmartCaptcha challenge have source references, but stable provider-principal and support CSRF/session probes remain unproven. CLI returns exit 7 before obtaining credentials or paying for captcha. Native captcha and headed fallback are not implemented. No session is promoted from caller-supplied principal labels. Existing sessions are preserved.
- **Private support mutations:** handlers and adapters exist, but CLI fails closed because production principal/CSRF verification is unavailable. Read-only session fixtures are a test boundary, not a supported login workaround.
- **Private support reads / sync and call details:** require an already promoted, account-bound session; this implementation cannot yet mint one through CLI login. Private reads are source-observed, not live-authenticated on this account. Calls show falls back to Data API.
- **Topics / contacts:** active-customer topics only; trial-topic discovery and phone/email selector subcommands are not implemented. Create requires existing explicit IDs.
- **Rapid / saved filters / reports:** execute documented read methods; account entitlement is not cached or independently probed. Provider refusals return provider/auth/quota errors, not an invented successful capability. `capability probe` currently reports static readiness, not a live entitlement test.
- **Archive:** crawlkit migration, exact crawlctl manifest interoperability, failed-page inventory, incremental resume, attachment bytes, pruning/deletion reconciliation and per-thread completion markers remain follow-up work. The compatible schema label does not certify crawlctl integration.
- **Secondary commands:** private balance and recording download are not implemented. Saved-filter writes, sales, recognition, exports, chats, CDR archives and originate remain out of scope.

The PR must remain draft while required production authentication work is incomplete. No live mutations may be used to prove these gaps without separate authorization.

## Review corrections

The implementation review caught and corrected default-profile fallthrough to a legacy token, logout crossing authentication surfaces, live search ignoring its positional query, writable archive initialization during live reads, and incomplete redaction of typed comment slices, nested parameter arrays and recording locators. Focused regression tests retain these contracts. Structured RPC error groups follow [UIS error handling](https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/): explicit auth/permission/quota/validation/unavailable rejections are distinct from unresolved writes.

Employee search uses `employees` / `jsquery` (`#.employee_id = ID`) per the provider array schema; legs keep their separate `employee_id` predicate. Local FTS search applies the requested page offset. Reopen checks known closure dates against the 14-day UI rule before dispatch; unknown dates remain subject to server validation.

Annotation commands resolve the selected session through `get.calls_report` and use its verified `communication_id` for the send-once write. Missing identity fails closed; dry-run describes the preflight without network. Config edits preserve unrequested on-disk credentials rather than persisting effective environment/profile credentials. Local search applies status before pagination.
