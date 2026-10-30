package cli

import (
	"encoding/json"
	"github.com/spf13/cobra"
	"os"
	"strings"
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
	page, e := c.SearchCalls(cmd.Context(), q)
	if e != nil {
		return true, operationError(e)
	}
	return true, emit(g, portal.Object{"source": "private.report_constructor", "data": archive.Scrub(page.Data), "metadata": page.Metadata, "limit": limit, "offset": offset, "date_from": from, "date_till": till, "limitations": []string{"Live offset page; no snapshot or historical completeness guarantee", "Cabinet communication_id; private filters use cabinet names and values"}})
}
