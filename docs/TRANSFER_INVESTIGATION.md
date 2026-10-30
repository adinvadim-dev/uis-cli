# Call and transfer investigation

Use the skill's existing account and private session. This recipe is read-only. Dates are literal cabinet wall time; timezone and historical completeness are unverified. With a supplied date, search that day. Otherwise start with today and yesterday using the host's current date, and state the window in the answer. An empty page means no matches in that window, not that the number has never called.

## Search

Resolve the installed skill directory to an absolute `SKILL_DIR`. Set `UIS_BIN` to the installed executable (`uis` or `$HOME/.local/bin/uis`), `ACCOUNT` to the requested account, `PHONE` to the supplied number, and `FROM` / `TILL` to explicit `YYYY-MM-DD HH:MM:SS` boundaries. Use shell variables as separate quoted arguments, not a string of combined flags.

Use pipeline error propagation (`set -o pipefail` in bash/zsh), `umask 077`, and a fresh private capture directory outside the repo. Raw files stay outside model context.

```sh
set -o pipefail
umask 077
CAPTURE_DIR=$(mktemp -d)
"$UIS_BIN" --account "$ACCOUNT" --json --timeout 12s --retries 0 \
  calls search --source private --phone "$PHONE" \
  --from "$FROM" --till "$TILL" --limit 20 \
  --sort call_start_date_time --order desc > "$CAPTURE_DIR/search.json"
python3 "$SKILL_DIR/scripts/call_brief.py" search < "$CAPTURE_DIR/search.json"
```

Inspect the CLI exit and envelope even if the command fails; the helper preserves typed errors without provider bodies. Resolve a CLI usage error rather than repeating it.

Bound search recovery to four read requests total, including pagination and retries. For a provider/transport failure, retry once with a one-day window. For an empty page, expand once to seven days ending at the original upper bound. If exact matching still errors or finds nothing, one alternate query may use a catalog-supported `like` filter on `contact_phone_number`, with digits only. Use targeted discovery below only if its operators are unknown; discovery counts toward the read budget. The 2026-10-30 trace found the target with `like`, but used a different date window too: this does not prove equality is broken. Keep query window and matching mode in the findings.

Construct raw filter JSON with a JSON encoder, preserving the phone as a string:

```sh
FILTER_JSON=$(python3 -c 'import json,sys; print(json.dumps({"field":"contact_phone_number","operator":"like","value":sys.argv[1]}))' "$PHONE")
"$UIS_BIN" --account "$ACCOUNT" --json --timeout 12s --retries 0 \
  calls search --source private --filter-json "$FILTER_JSON" \
  --from "$FROM" --till "$TILL" --limit 20 \
  --sort call_start_date_time --order desc > "$CAPTURE_DIR/search-alternate.json"
python3 "$SKILL_DIR/scripts/call_brief.py" search < "$CAPTURE_DIR/search-alternate.json"
```

The alternate replaces `--phone`; both together would be ANDed. A `--virtual-number` search is for a known virtual number, not a second guess for a caller's phone. Select at most three relevant calls, preferring transfers to the requested destination; inspect related attempts only when they explain a failure. If the relevant call is beyond the page/budget, state that limitation. `total_items: null` is unknown, not zero.

## Details and attribution

Use the selected cabinet `communication_id`; `calls show` verifies its returned identity and exposes source/fallback. `calls legs` requires Data API access. With `CALL_ID` set to the selected ID:

```sh
"$UIS_BIN" --account "$ACCOUNT" --json --timeout 12s --retries 0 \
  calls show "$CALL_ID" --from "$FROM" --till "$TILL" \
  > "$CAPTURE_DIR/detail-$CALL_ID.json"
python3 "$SKILL_DIR/scripts/call_brief.py" details \
  < "$CAPTURE_DIR/detail-$CALL_ID.json"
```

Allow at most three detail requests. The helper keeps nested events with parent paths; `omitted` or `truncated_strings` means the projection is incomplete. Read a targeted next page with `details --offset N --limit N` from the same capture when needed. A reduced Data API fallback or unsupported shape is insufficient for a full private timeline; disclose it. Stop once the requested facts are supported.

Distinguish who initiated the transfer from who ended each leg. Quote the termination event with its time and leg/participant context. Label an unidentified side as unknown; a short connection alone does not prove rejection or a technical fault. Separate explicit provider failures from interpretation. Final answer: searched date/window, relevant call IDs/times, transfer initiator/destination, connected duration, termination evidence, and unresolved limitations.

## Targeted catalog and errors

```sh
"$UIS_BIN" --account "$ACCOUNT" --json --timeout 12s --retries 0 \
  filters list --source private --report-type calls > "$CAPTURE_DIR/filters.json"
python3 "$SKILL_DIR/scripts/call_brief.py" filters --field contact_phone_number \
  < "$CAPTURE_DIR/filters.json"
```

Default catalog output includes field identity/type/operators, not account-wide values. Add `--values --offset N --limit N` only for the required employee/scenario field. Keep the full catalog in its private file.

Exit 3 / explicit auth failure: inspect `auth status`; refresh once if the session is missing/expired. Exit 6 / provider error: follow the bounded window/retry route above. Exit 8 / rate limit: stop and report it. Permission or unavailable capability: report the missing access. Refresh can invoke captcha and take minutes; it is not a generic transport recovery. After 60 seconds without useful data, send a progress message with the failing stage; after the request budget, return partial findings rather than continuing an open-ended search.
