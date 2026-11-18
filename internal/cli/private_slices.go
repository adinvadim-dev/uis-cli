package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"uis-cli/internal/archive"
	"uis-cli/internal/portal"
)

const (
	cabinetTimeLayout = "2006-01-02 15:04:05"
	maxSliceWindows   = 744 // one month of hourly windows
)

// sliceWindow is one closed cabinet wall-time window [From, Till].
type sliceWindow struct {
	From string `json:"from"`
	Till string `json:"till"`
}

// splitSliceWindows splits a closed --from/--till interval into consecutive
// closed windows of the given length; the last one ends at till. Times are
// cabinet wall time and are never converted between zones.
func splitSliceWindows(from, till, raw string) ([]sliceWindow, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Minute || d%time.Minute != 0 {
		return nil, exitf(2, "--slice must be a whole number of minutes, at least 1m (for example 1h or 30m)")
	}
	start, err := time.Parse(cabinetTimeLayout, from)
	if err != nil {
		return nil, exitf(2, "--from must be YYYY-MM-DD HH:MM:SS with --slice")
	}
	end, err := time.Parse(cabinetTimeLayout, till)
	if err != nil {
		return nil, exitf(2, "--till must be YYYY-MM-DD HH:MM:SS with --slice")
	}
	if end.Before(start) {
		return nil, exitf(2, "--till must not be before --from")
	}
	if n := end.Sub(start)/d + 1; n > maxSliceWindows {
		return nil, exitf(2, "--slice %s splits the window into more than %d windows; use a longer slice or a shorter period", raw, maxSliceWindows)
	}
	var out []sliceWindow
	for !start.After(end) {
		last := start.Add(d - time.Second)
		if last.After(end) {
			last = end
		}
		out = append(out, sliceWindow{start.Format(cabinetTimeLayout), last.Format(cabinetTimeLayout)})
		start = last.Add(time.Second)
	}
	return out, nil
}

// transientSliceError reports failures worth one more attempt on a window:
// cabinet transport/size failures and an unstable population during the read.
func transientSliceError(e error) bool {
	var ee *ExitError
	if !errors.As(e, &ee) {
		return false
	}
	switch ee.Details["stage"] {
	case "cabinet_transport", "cabinet_response_size":
		return true
	}
	if ee.Code != 7 {
		return false
	}
	switch ee.Details["reason"] {
	case "population_changed", "repeated_communication_id", "count_mismatch", "short_page":
		return true
	}
	return false
}

// collectPrivateSlices reads every window with its own --all budget and merges
// them; any window that still fails after one transient retry fails the whole
// command without data.
func collectPrivateSlices(cmd *cobra.Command, g *globalFlags, c *portal.Client, openErr error, q portal.CallQuery, maxPages int, sliceRaw string, windows []sliceWindow) (portal.Object, error) {
	started := time.Now().UTC().Format(time.RFC3339)
	rows := []portal.Object{}
	seen := map[string]bool{}
	slices := []portal.Object{}
	var sum int64
	totalKnown := true
	complete := true
	pages, empty, retried, duplicates := 0, 0, 0, 0
	for i, w := range windows {
		wq := q
		wq.From, wq.Till = w.From, w.Till
		var got privateCollection
		read := func(cl *portal.Client) error {
			c = cl // a refreshed session carries over to later windows
			var re error
			got, re = collectPrivateCallPages(cmd, cl, wq, maxPages)
			return re
		}
		err := retryPrivateRead(cmd.Context(), g, c, openErr, read)
		openErr = nil
		attempts := 1
		if err != nil && transientSliceError(err) {
			retried++
			attempts++
			err = retryPrivateRead(cmd.Context(), g, c, nil, read)
		}
		if err != nil {
			return nil, sliceFailure(err, w, i, len(windows), attempts)
		}
		emptyUnknown := len(got.rows) == 0 && got.reason == "provider_count_unavailable"
		if emptyUnknown {
			empty++
		}
		if !got.complete() && !emptyUnknown {
			complete = false
		}
		if got.total != nil {
			sum += *got.total
		} else if len(got.rows) > 0 {
			totalKnown = false
		}
		pages += got.pages
		for _, row := range got.rows {
			id := string(row["communication_id"].(json.Number))
			if seen[id] {
				duplicates++
				continue
			}
			seen[id] = true
			rows = append(rows, row)
		}
		slices = append(slices, portal.Object{"from": w.From, "till": w.Till, "complete": got.complete(), "reason": got.reason, "returned": len(got.rows), "reported_total": got.total, "pages": got.pages, "attempts": attempts})
	}
	var reported interface{}
	if totalKnown {
		reported = sum
	}
	reason := "slices_complete"
	if !complete {
		reason = "slice_incomplete"
	}
	return portal.Object{
		"source": "private.report_constructor", "data": archive.Scrub(rows),
		"date_from": q.From, "date_till": q.Till, "fields": q.Fields,
		"query_scope": portal.Object{"filter": q.Filter, "slice": sliceRaw},
		"coverage": portal.Object{"complete": complete, "reason": reason, "returned": len(rows), "reported_total": reported, "pages": pages,
			"initial_offset": 0, "next_offset": nil, "snapshot_isolation": false,
			"windows": len(windows), "empty_windows": empty, "retried_windows": retried, "duplicates_removed": duplicates, "slices": slices},
		"observation": portal.Object{"started_at": started, "finished_at": time.Now().UTC().Format(time.RFC3339)},
		"limitations": append(append([]string{}, privateAllLimitations...), "Each slice is a separate observation; an empty slice has no provider total and counts as complete"),
	}, nil
}

// sliceFailure keeps the window's error code, message and details and adds
// which window failed and how far the collection got.
func sliceFailure(e error, w sliceWindow, index, windows, attempts int) error {
	var ee *ExitError
	if !errors.As(e, &ee) {
		ee = &ExitError{Code: 1, Msg: e.Error()}
	}
	details := map[string]interface{}{}
	for k, v := range ee.Details {
		details[k] = v
	}
	details["failed_slice"] = map[string]interface{}{"from": w.From, "till": w.Till, "attempts": attempts}
	details["windows"] = windows
	details["windows_completed"] = index
	return &ExitError{Code: ee.Code, Msg: fmt.Sprintf("%s; --slice window %s – %s failed after %d attempt(s), %d of %d windows completed, no data returned", ee.Msg, w.From, w.Till, attempts, index, windows), Details: details}
}
