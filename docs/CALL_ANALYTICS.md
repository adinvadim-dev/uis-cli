# Call counts, keypresses and robot attribution

Read-only recipe for questions over many calls: “how many calls”, “who pressed 2”,
“which calls did the robot number make”. Reuse the variables and private capture
directory from [the investigation recipe](TRANSFER_INVESTIGATION.md) (`UIS_BIN`,
`ACCOUNT`, `SKILL_DIR`, `FROM`/`TILL`, `umask 077`, `CAPTURE_DIR`). State the
window and filters in the answer.

## Count a period with one bounded export

```sh
"$UIS_BIN" --account "$ACCOUNT" --json --timeout 20s --retries 0 \
  calls search --source private --analysis --all \
  --from "$FROM" --till "$TILL" --sort communication_id --order asc \
  --limit 1000 --max-pages 25 > "$CAPTURE_DIR/period.json"
```

Read `data.coverage`: a total is reportable only when `complete` is true. With
`complete: false`, report `returned`, `reported_total`, `reason` and
`next_offset` as an incomplete count. A drift, duplicate-ID or short-page error
means the provider total moved during the read: report it; a rerun is a new
observation with its own collection time. `--all` replaces manual `--offset`
paging, which has no snapshot guarantee. See
[bounded private paging](CLI_SPEC.md#operator-analysis-fields-and-bounded-private-paging).

## Calls of a robot or outbound number

The private catalog field `employee_phone_number` («Номер сотрудника», format
`text`) matches the employee-side number of a call, for example an autodial
line such as `05XXXXX`. The `employees` field matches cabinet names, not numbers.

```sh
FILTER_JSON=$(python3 -c 'import json,sys; print(json.dumps({"field":"employee_phone_number","operator":"=","value":sys.argv[1]}))' "$EMPLOYEE_NUMBER")
"$UIS_BIN" --account "$ACCOUNT" --json --timeout 20s --retries 0 \
  calls search --source private --filter-json "$FILTER_JSON" --analysis --all \
  --from "$FROM" --till "$TILL" --sort communication_id --order asc \
  --limit 1000 > "$CAPTURE_DIR/employee-number.json"
```

Observed live 2026-11-10: `=` and `like` on an outbound employee number returned
the same total; `employees like <number>` returned 0. Confirm operators with
`call_brief.py filters --field employee_phone_number` over a saved `filters list`
capture. An empty result is “no calls in this window”, not “number unknown”.

## Keypresses (DTMF)

Count keys from structured `dtmf` timeline events, one detail read per eligible
call. Eligible calls are every ID in a complete export above; apply a duration or
status filter only when the user asked for it, and name it.

```sh
python3 -c 'import json,sys; [print(r["communication_id"]) for r in json.load(sys.stdin)["data"]["data"]]' \
  < "$CAPTURE_DIR/period.json" > "$CAPTURE_DIR/ids.txt"
while read -r CALL_ID; do
  "$UIS_BIN" --account "$ACCOUNT" --json --timeout 12s --retries 0 \
    calls show "$CALL_ID" --from "$FROM" --till "$TILL" \
    < /dev/null > "$CAPTURE_DIR/detail-$CALL_ID.json"
done < "$CAPTURE_DIR/ids.txt"
python3 "$SKILL_DIR/scripts/call_brief.py" dtmf --summary "$CAPTURE_DIR"/detail-*.json
```

`dtmf` walks nested events, deduplicates by call ID, event path and time, and
returns `calls_scanned`, `calls_with_dtmf`, `key_counts` (events),
`calls_by_key` (distinct calls) and `sequence_counts`. Drop `--summary` for
per-call events with masked party labels. Failed or reduced captures appear in
`failed` with a reason and are never zero-key calls; unrecognised DTMF-like text
appears in `unparsed`. Either makes `complete: false` and exit 1: re-read the
failed IDs once, then report the count as incomplete with scanned/failed numbers.

## Who is the robot

Call a party «робот» only when both are established: its identity (autodial
`call_type.value_id=auto_out_calls`, or the employee/number bound to an autodial
campaign via `autodial get`/`autodial employees`) and the terminating leg that
it ended. A bare numeric participant or a generic disconnect is «неизвестная
сторона»; name what is missing.
