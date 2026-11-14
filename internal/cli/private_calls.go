package cli

import (
	"context"
	"encoding/json"
	"github.com/spf13/cobra"
	"os"
	"strings"
	"time"
	"uis-cli/internal/archive"
	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func callSource(cmd *cobra.Command) (string, error) {
	s, _ := cmd.Flags().GetString("source")
	if s != "auto" && s != "private" && s != "dataapi" {
		return "", exitf(2, "source must be auto, private or dataapi")
	}
	return s, nil
}
func selectCallClient(g *globalFlags, source string) (*portal.Client, error) {
	if source == "dataapi" {
		return nil, nil
	}
	c, err := privateClient(g)
	if err != nil && source == "auto" {
		// Only absence permits the compatibility fallback. A mismatched existing
		// session must not silently switch credentials or accounts.
		name, _, e := profileFor(g)
		if e != nil {
			return nil, e
		}
		if _, e = portal.LoadSession(profile.State(name)); os.IsNotExist(e) {
			return nil, nil
		}
	}
	return c, err
}
func privateCallQuery(cmd *cobra.Command, from, till string, limit, offset int) (portal.CallQuery, error) {
	q := portal.CallQuery{ReportType: "calls", Mode: "flat", From: from, Till: till, Limit: limit, Offset: offset,
		Fields: []string{"communication_id", "call_start_date_time", "call_direction", "contact_phone_number", "virtual_phone_number", "call_status", "employees", "call_scenario", "call_is_transfer"}}
	analysis, _ := cmd.Flags().GetBool("analysis")
	if analysis {
		q.Fields = append(q.Fields, "call_talk_duration", "call_total_duration", "call_type", "is_mass_auto_call_enabled")
	}
	filters := []interface{}{}
	add := func(field string, v interface{}) {
		filters = append(filters, portal.Object{"field": field, "operator": "=", "value": v})
	}
	for _, flag := range []string{"employee-id", "scenario-id"} {
		v, _ := cmd.Flags().GetString(flag)
		if v != "" {
			return q, exitf(7, "Private report filters expose names; use --employee or --scenario (see filters list --source private)")
		}
	}
	for flag, field := range map[string]string{"phone": "contact_phone_number", "virtual-number": "virtual_phone_number", "employee": "employees", "scenario": "call_scenario"} {
		v, _ := cmd.Flags().GetString(flag)
		if v != "" {
			if flag == "phone" {
				add(field, v)
			} else {
				add(field, []string{v})
			}
		}
	}
	id, _ := cmd.Flags().GetString("id")
	if id != "" {
		n, err := positive(id)
		if err != nil {
			return q, err
		}
		add("communication_id", n)
	}
	direction, _ := cmd.Flags().GetString("direction")
	if direction != "" {
		v := "Входящий звонок"
		if direction == "out" {
			v = "Исходящий звонок"
		}
		add("call_direction", []string{v})
	}
	for flag, field := range map[string]string{"lost": "call_status", "transfer": "call_is_transfer"} {
		if cmd.Flags().Changed(flag) {
			b, _ := cmd.Flags().GetBool(flag)
			v := "Принятый"
			if b {
				v = "Потерянный"
			}
			if flag == "transfer" {
				v = "Нет"
				if b {
					v = "Да"
				}
			}
			add(field, []string{v})
		}
	}
	raw, _ := cmd.Flags().GetString("filter-json")
	if raw != "" {
		var f map[string]interface{}
		d := json.NewDecoder(strings.NewReader(raw))
		d.UseNumber()
		if d.Decode(&f) != nil || f == nil {
			return q, exitf(2, "filter-json must be an object")
		}
		filters = append(filters, f)
	}
	if len(filters) == 1 {
		q.Filter = filters[0]
	} else if len(filters) > 1 {
		q.Filter = portal.Object{"condition": "and", "filters": filters}
	}
	sort, _ := cmd.Flags().GetString("sort")
	order, _ := cmd.Flags().GetString("order")
	// Start time is not unique, so offset pages over it repeat or skip calls; --all defaults to the unique ID.
	if all, _ := cmd.Flags().GetBool("all"); all && !cmd.Flags().Changed("sort") {
		sort = "communication_id"
		if !cmd.Flags().Changed("order") {
			order = "asc"
		}
	}
	if sort != "call_start_date_time" && sort != "communication_id" {
		return q, exitf(2, "Private sort must be call_start_date_time or communication_id")
	}
	if order != "asc" && order != "desc" {
		return q, exitf(2, "order must be asc or desc")
	}
	q.Sort = []portal.CallSort{{Field: sort, Order: order}}
	return q, nil
}
func runPrivateSearch(g *globalFlags, cmd *cobra.Command, source, from, till string, limit, offset int) (bool, error) {
	all, _ := cmd.Flags().GetBool("all")
	analysis, _ := cmd.Flags().GetBool("analysis")
	maxPages, _ := cmd.Flags().GetInt("max-pages")
	if maxPages < 1 || maxPages > 100 || (cmd.Flags().Changed("max-pages") && !all) {
		return true, exitf(2, "--max-pages requires --all and a value from 1 to 100")
	}
	if (all || analysis) && source != "private" {
		return true, exitf(2, "--all and --analysis require explicit --source private")
	}
	if all && (!cmd.Flags().Changed("from") || !cmd.Flags().Changed("till") || cmd.Flags().Changed("date-hint")) {
		return true, exitf(2, "--all requires explicit --from and --till, without --date-hint")
	}
	if source == "dataapi" {
		return false, nil
	}
	// Validate the explicit private query before any session I/O.
	q, err := privateCallQuery(cmd, from, till, limit, offset)
	if source == "private" && err != nil {
		return true, err
	}
	if g.DryRun {
		if source == "private" {
			return true, emit(g, portal.Object{"source": "private.report_constructor", "query": q, "network": false})
		}
		return false, nil
	}
	c, e := selectCallClient(g, source)
	if e != nil {
		return true, e
	}
	if c == nil {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if all {
		return true, collectPrivateCalls(g, cmd, c, q, maxPages)
	}
	page, e := c.SearchCalls(cmd.Context(), q)
	if e != nil {
		return true, operationError(e)
	}
	return true, emit(g, portal.Object{"source": "private.report_constructor", "data": archive.Scrub(page.Data), "metadata": page.Metadata, "limit": limit, "offset": offset, "date_from": from, "date_till": till, "limitations": []string{"Live offset page; no snapshot or historical completeness guarantee", "Cabinet communication_id; private filters use cabinet names and values"}})
}

func collectPrivateCalls(g *globalFlags, cmd *cobra.Command, c *portal.Client, q portal.CallQuery, maxPages int) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 150*time.Second)
	defer cancel()
	started := time.Now().UTC().Format(time.RFC3339)
	rows := []portal.Object{}
	seen := map[string]bool{}
	var total *int64
	initialOffset, pages := q.Offset, 0
	ended, reason := false, "page_limit"
	// JSON output replaces exit-7 messages with a generic one; the reason must survive in details.
	unstable := func(reason, msg string) *ExitError {
		e := exitf(7, "%s", msg)
		e.Details = map[string]interface{}{"reason": reason, "pages_read": pages, "rows_discarded": len(rows), "hint": "The call set moved during --all; nothing was returned. Rerun --all with --sort communication_id --order asc over a closed window, or report the count as unavailable. Not an auth or capability problem; do not page manually with --offset."}
		return e
	}
	for pages < maxPages && len(rows) < 50000 {
		if remaining := 50000 - len(rows); q.Limit > remaining {
			q.Limit = remaining
		}
		page, err := c.SearchCalls(ctx, q)
		if err != nil {
			return operationError(err)
		}
		pages++
		if total != nil && page.Metadata.Total != nil && *total != *page.Metadata.Total {
			return unstable("population_changed", "Call population changed between pages; retain previous snapshot and retry")
		}
		if page.Metadata.Total != nil {
			total = page.Metadata.Total
		}
		for _, row := range page.Data {
			id := string(row["communication_id"].(json.Number)) // SearchCalls validates identity.
			if seen[id] {
				return unstable("repeated_communication_id", "Repeated communication ID across pages; population is unstable")
			}
			seen[id] = true
			rows = append(rows, row)
		}
		q.Offset += len(page.Data)
		if total != nil && int64(q.Offset) > *total {
			return unstable("count_mismatch", "Provider count disagrees with returned call rows")
		}
		if total != nil && int64(q.Offset) == *total {
			ended, reason = true, "provider_count_reached"
			break
		}
		if len(page.Data) < q.Limit {
			if total != nil {
				return unstable("short_page", "Short call page before provider count; population is incomplete")
			}
			reason = "provider_count_unavailable"
			break
		}
	}
	if !ended && len(rows) == 50000 {
		reason = "record_limit"
	}
	var next interface{} = q.Offset
	if ended || reason == "provider_count_unavailable" {
		next = nil
	}
	return emit(g, portal.Object{
		"source": "private.report_constructor", "data": archive.Scrub(rows),
		"date_from": q.From, "date_till": q.Till, "fields": q.Fields,
		"query_scope": portal.Object{"filter": q.Filter},
		"coverage":    portal.Object{"complete": ended && initialOffset == 0, "reason": reason, "returned": len(rows), "reported_total": total, "pages": pages, "initial_offset": initialOffset, "next_offset": next, "snapshot_isolation": false},
		"observation": portal.Object{"started_at": started, "finished_at": time.Now().UTC().Format(time.RFC3339)},
		"limitations": []string{"Cabinet wall time: verify account timezone before calling this a Moscow interval", "Private communication_id is not a verified classic session ID", "Stable count is not snapshot isolation; keep each observation separately", "MAV flag and call type are raw evidence, not inferred manual/key/CRM origins"},
	})
}
