# Bounded autodial conversion reporting

Use the [read-only summary helper](../scripts/autodial_summary.py) for a scheduled short report instead of rediscovering payloads and parsing cached artifacts on every execution:

```sh
python3 <skill-root>/scripts/autodial_summary.py --uis-bin /path/to/uis --account ACCOUNT
```

The helper prints JSON and never sends messages or changes a schedule. An authorized scheduler can deliver `message_text` only when `should_send=true`, to its existing approved audience. Preserve the audience's mention and weekend rules. A helper result is not a provider delivery receipt.

The day and weekend gate always use `Europe/Moscow`, derived from an offset-aware instant. Friday 17:30 UTC is Friday 20:30 MSK even when the host already shows Saturday. Saturday in Moscow skips with zero API calls. An empty successful campaign list produces an explicit empty-state message; an unavailable list or calls read produces a failure message and never becomes zero activity.

One fresh private autodial list uses the Moscow date and `in_process` status. Up to three calls reads run concurrently. All subprocesses, queued reads, and at most one shared private-session refresh consume the same 150-second deadline; the scheduler should allow at least 180 seconds. Session presence alone does not establish live access. Refresh occurs only after an authentication failure, never after arbitrary provider errors.

Attempts are returned call rows, reached conversations have `talk_duration > 0`, and the transfer numerator counts rows with a mark **containing** `Перевод`. These are UIS marks, not confirmed map transfers. The message retains that distinction and the lack of historical completeness guarantees. No raw call rows, subscriber phones, provider stderr or credential-bearing error messages are emitted.

For a read-only historical proof:

```sh
python3 <skill-root>/scripts/autodial_summary.py --account ACCOUNT --date 2026-11-06
```

The date constrains the provider campaign-list query; campaign status is read at execution time. `campaign_status_basis=current` and the historical message explicitly identify this scope. A completed or paused campaign disappears from the `in_process` selection even when it was running on the chosen date. The helper does not reconstruct past statuses or a complete day-of-calls snapshot; compare exact campaign IDs and their current statuses when a replay changes.

Historical mode always sets `should_send=false`, including on a weekday. `--now` accepts an offset-aware ISO instant for clock verification and also forces `should_send=false`. Neither mode proves a scheduled execution or actual delivery. Keep the live invocation, installed provenance, account, source limitations, elapsed time and separate scheduler/provider receipt in rollout evidence.
