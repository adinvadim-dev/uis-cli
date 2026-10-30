package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var campaignWritableFields = strings.Fields("name phone_list employee_ids first_call_direction begin_date end_date number_capacity_usage_rule number_capacity_id number_capacity_group_id pure_ani_phone_list_id good_call_min_duration after_call_employee_busy_max_duration subscriber_dialing_timeout funnel_mark_ids auto_mark_id target_auto_mark_id attempts subscriber_time_from subscriber_time_till is_use_subscriber_timezone is_outside_processing_enabled is_predict_enabled subscriber_to_employee_ratio predicted_talk_duration is_subscriber_message_enabled subscriber_playlist")

func campaignObject(v interface{}) (Object, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, &Error{Code: 2, Stage: "campaign_json"}
	}
	var o Object
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&o) != nil || o == nil {
		return nil, &Error{Code: 2, Stage: "campaign_object"}
	}
	return o, nil
}
func campaignID(v interface{}) string {
	s := fmt.Sprint(v)
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n <= 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}
func campaignIDs(v interface{}) ([]string, bool) {
	a, ok := v.([]interface{})
	if !ok || len(a) == 0 {
		return nil, false
	}
	out := []string{}
	for _, x := range a {
		id := campaignID(x)
		if id == "" {
			return nil, false
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, true
}
func campaignPhones(v interface{}) ([]Object, bool) {
	a, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := []Object{}
	seen := map[string]bool{}
	for _, x := range a {
		o, ok := x.(map[string]interface{})
		if !ok {
			return nil, false
		}
		n, ok := o["numa"].(string)
		if !ok || strings.TrimSpace(n) == "" || seen[n] {
			return nil, false
		}
		for _, k := range []string{"name", "comment"} {
			if v, ok := o[k]; ok && v != nil {
				if _, ok := v.(string); !ok {
					return nil, false
				}
			}
		}
		seen[n] = true
		out = append(out, o)
	}
	return out, true
}

// ValidateCampaignInput is shared by dry-run and dispatch. In particular, an
// empty draft is not a create contract: the cabinet's create action schedules a
// campaign and requires contacts, employees, date and a configured caller ID.
func ValidateCampaignInput(action string, input Object) error {
	p, e := campaignObject(input)
	if e != nil {
		return e
	}
	allowed := map[string]bool{}
	for _, k := range campaignWritableFields {
		allowed[k] = true
	}
	for k := range p {
		if !allowed[k] {
			return &Error{Code: 2, Stage: "campaign_readonly_or_unknown_field"}
		}
	}
	if len(p) == 0 {
		return &Error{Code: 2, Stage: "campaign_empty_patch"}
	}
	for _, k := range []string{"is_use_subscriber_timezone", "is_outside_processing_enabled", "is_predict_enabled", "is_subscriber_message_enabled"} {
		if v, ok := p[k]; ok {
			if _, ok := v.(bool); !ok {
				return &Error{Code: 2, Stage: "campaign_boolean"}
			}
		}
	}
	for _, k := range []string{"good_call_min_duration", "after_call_employee_busy_max_duration", "subscriber_dialing_timeout", "subscriber_to_employee_ratio", "predicted_talk_duration"} {
		if v, ok := p[k]; ok && v != nil {
			n, ok := v.(json.Number)
			if !ok {
				return &Error{Code: 2, Stage: "campaign_number"}
			}
			f, e := n.Float64()
			if e != nil || f < 0 {
				return &Error{Code: 2, Stage: "campaign_number"}
			}
		}
	}
	if v, ok := p["name"]; ok {
		if n, ok := v.(string); !ok || strings.TrimSpace(n) == "" {
			return &Error{Code: 2, Stage: "campaign_name_required"}
		}
	}
	if v, ok := p["first_call_direction"]; ok && v != "contact" && v != "employee" {
		return &Error{Code: 2, Stage: "campaign_call_direction"}
	}
	for _, k := range []string{"subscriber_time_from", "subscriber_time_till"} {
		if v, ok := p[k]; ok {
			if _, e := time.Parse("15:04", fmt.Sprint(v)); e != nil {
				return &Error{Code: 2, Stage: "campaign_subscriber_time"}
			}
		}
	}
	if v, ok := p["attempts"]; ok {
		o, ok := v.(map[string]interface{})
		if !ok {
			return &Error{Code: 2, Stage: "campaign_attempts"}
		}
		for k, v := range o {
			switch k {
			case "subscriber_busy", "subscriber_not_responsible", "subscriber_number_problems", "other":
			default:
				return &Error{Code: 2, Stage: "campaign_attempts"}
			}
			a, ok := v.([]interface{})
			if !ok {
				return &Error{Code: 2, Stage: "campaign_attempts"}
			}
			for _, v := range a {
				if !campaignInterval(fmt.Sprint(v)) {
					return &Error{Code: 2, Stage: "campaign_attempt_interval"}
				}
			}
		}
	}
	if v, ok := p["subscriber_playlist"]; ok && v != nil {
		o, ok := v.(map[string]interface{})
		if !ok {
			return &Error{Code: 2, Stage: "campaign_playlist"}
		}
		items, ok := o["items"].([]interface{})
		if !ok {
			return &Error{Code: 2, Stage: "campaign_playlist"}
		}
		for _, v := range items {
			o, ok := v.(map[string]interface{})
			if !ok {
				return &Error{Code: 2, Stage: "campaign_playlist"}
			}
			text, tts := o["tts_string"].(string)
			if tts && (strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 1000) {
				return &Error{Code: 2, Stage: "campaign_tts_text"}
			}
			if campaignID(o["media_file_id"]) == "" && !tts {
				return &Error{Code: 2, Stage: "campaign_playlist"}
			}
		}
	}
	if v, ok := p["employee_ids"]; ok {
		if _, ok := campaignIDs(v); !ok {
			return &Error{Code: 2, Stage: "campaign_employees_required"}
		}
	}
	if v, ok := p["phone_list"]; ok {
		if _, ok := campaignPhones(v); !ok {
			return &Error{Code: 2, Stage: "campaign_phone_list"}
		}
	}
	if v, ok := p["begin_date"]; ok && v == nil {
		return &Error{Code: 2, Stage: "campaign_begin_date_required"}
	}
	for _, k := range []string{"begin_date", "end_date"} {
		if v, ok := p[k]; ok && v != nil {
			if s, ok := v.(string); !ok {
				return &Error{Code: 2, Stage: "campaign_date"}
			} else if _, e := time.Parse("2006-01-02 15:04:05", s); e != nil {
				return &Error{Code: 2, Stage: "campaign_date"}
			}
		}
	}
	if action == "create" {
		n, ok := p["name"].(string)
		if !ok || strings.TrimSpace(n) == "" {
			return &Error{Code: 2, Stage: "campaign_name_required"}
		}
		ids, ok := campaignIDs(p["employee_ids"])
		if !ok || len(ids) == 0 {
			return &Error{Code: 2, Stage: "campaign_employees_required"}
		}
		phones, ok := campaignPhones(p["phone_list"])
		if !ok || len(phones) == 0 {
			return &Error{Code: 2, Stage: "campaign_contacts_required"}
		}
		if s, ok := p["begin_date"].(string); !ok || s == "" {
			return &Error{Code: 2, Stage: "campaign_begin_date_required"}
		}
		rule, _ := p["number_capacity_usage_rule"].(string)
		switch rule {
		case "fixed":
			if campaignID(p["number_capacity_id"]) == "" {
				return &Error{Code: 2, Stage: "campaign_caller_required"}
			}
		case "random", "random_from_group":
			if campaignID(p["number_capacity_group_id"]) == "" {
				return &Error{Code: 2, Stage: "campaign_caller_group_required"}
			}
		case "carousel":
			if campaignID(p["pure_ani_phone_list_id"]) == "" {
				return &Error{Code: 2, Stage: "campaign_caller_list_required"}
			}
		default:
			return &Error{Code: 2, Stage: "campaign_caller_rule_required"}
		}
	}
	return nil
}
func campaignInterval(s string) bool {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return false
	}
	for i, p := range parts {
		if len(p) < 2 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
		n, e := strconv.ParseUint(p, 10, 64)
		if e != nil || (i > 0 && n > 59) {
			return false
		}
	}
	return true
}
func campaignDefaults() Object {
	return Object{"phone_list": []interface{}{}, "employee_ids": []interface{}{}, "number_capacity_group_id": nil, "number_capacity_id": nil, "pure_ani_phone_list_id": nil, "end_date": nil, "is_use_subscriber_timezone": false, "subscriber_time_from": "10:00", "subscriber_time_till": "20:00", "good_call_min_duration": 30, "after_call_employee_busy_max_duration": 20, "subscriber_dialing_timeout": 23, "attempts": Object{"subscriber_busy": []string{"00:15:00", "00:30:00"}, "subscriber_not_responsible": []string{"00:15:00", "00:30:00"}, "subscriber_number_problems": []string{"00:15:00", "00:30:00"}, "other": []string{"00:15:00", "00:30:00"}}, "first_call_direction": "contact", "is_outside_processing_enabled": false, "is_predict_enabled": false, "subscriber_to_employee_ratio": 1, "predicted_talk_duration": 60, "is_subscriber_message_enabled": false, "subscriber_playlist": nil, "funnel_mark_ids": []interface{}{}, "auto_mark_id": nil, "target_auto_mark_id": nil}
}

func (c *Client) AutodialTimezone(ctx context.Context) (*time.Location, error) {
	v, e := c.cabinetRead(ctx, true, Object{"jsonrpc": "2.0", "id": "uis-autodial-account", "method": "get.account", "params": Object{}})
	if e != nil {
		return nil, e
	}
	result, ok := v["result"].(map[string]interface{})
	if !ok || v["id"] != "uis-autodial-account" {
		return nil, &Error{Code: 7, Stage: "campaign_account_timezone"}
	}
	rows, ok := result["data"].([]interface{})
	if !ok || len(rows) != 1 {
		return nil, &Error{Code: 7, Stage: "campaign_account_timezone"}
	}
	row, ok := rows[0].(map[string]interface{})
	if !ok {
		return nil, &Error{Code: 7, Stage: "campaign_account_timezone"}
	}
	zone, ok := row["timezone"].(string)
	if !ok {
		return nil, &Error{Code: 7, Stage: "campaign_account_timezone"}
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return nil, &Error{Code: 7, Stage: "campaign_account_timezone"}
	}
	return loc, nil
}
func (c *Client) campaignMatches(ctx context.Context, name string) (map[string]Object, error) {
	v, e := c.Autodial(ctx, "list", "", Object{"search_text": name, "statuses": []string{}})
	if e != nil {
		return nil, e
	}
	rows := v.([]interface{})
	out := map[string]Object{}
	for _, v := range rows {
		o, ok := v.(map[string]interface{})
		if !ok {
			return nil, &Error{Code: 7, Stage: "campaign_list_row"}
		}
		if o["name"] == name {
			id := ID(o)
			if !validCampaignID(id) {
				return nil, &Error{Code: 7, Stage: "campaign_list_identity"}
			}
			out[id] = o
		}
	}
	return out, nil
}
func campaignBody(input, current Object, loc *time.Location) (Object, error) {
	body := campaignDefaults()
	for _, k := range campaignWritableFields {
		if v, ok := current[k]; ok {
			body[k] = v
		}
	}
	if current != nil {
		for _, k := range []string{"begin_date", "end_date"} {
			if s, ok := body[k].(string); ok {
				if d, e := time.Parse(time.RFC3339, s); e == nil {
					body[k] = d.In(loc).Format("2006-01-02 15:04:05")
				}
			}
		}
		if phones, ok := campaignPhones(body["phone_list"]); ok {
			active := []Object{}
			for _, p := range phones {
				if p["status_mnemonic"] != "deleted" {
					active = append(active, Object{"numa": p["numa"], "name": p["name"], "comment": p["comment"]})
				}
			}
			body["phone_list"] = active
		}
	}
	for k, v := range input {
		if k == "attempts" {
			if old, ok := body[k].(map[string]interface{}); ok {
				for a, b := range v.(map[string]interface{}) {
					old[a] = b
				}
				continue
			}
			if old, ok := body[k].(Object); ok {
				for a, b := range v.(map[string]interface{}) {
					old[a] = b
				}
				continue
			}
		}
		body[k] = v
	}
	if body["number_capacity_usage_rule"] == "random_from_group" {
		body["number_capacity_usage_rule"] = "random"
	}
	if body["number_capacity_usage_rule"] == "random" {
		body["number_capacity_id"] = nil
		body["pure_ani_phone_list_id"] = nil
	} else if body["number_capacity_usage_rule"] == "carousel" {
		body["number_capacity_id"] = nil
		body["number_capacity_group_id"] = nil
	} else {
		body["number_capacity_group_id"] = nil
		body["pure_ani_phone_list_id"] = nil
	}
	if fmt.Sprint(body["after_call_employee_busy_max_duration"]) == "0" {
		body["after_call_employee_busy_max_duration"] = nil
	}
	for _, k := range []string{"number_capacity_id", "number_capacity_group_id", "pure_ani_phone_list_id", "auto_mark_id", "target_auto_mark_id"} {
		if body[k] != nil {
			if id := campaignID(body[k]); id != "" {
				body[k] = json.Number(id)
			} else {
				return nil, &Error{Code: 2, Stage: "campaign_reference_id"}
			}
		}
	}
	if ids, ok := campaignIDs(body["employee_ids"]); ok {
		normalized := make([]interface{}, len(ids))
		for i, id := range ids {
			normalized[i] = json.Number(id)
		}
		body["employee_ids"] = normalized
	}
	body["status"] = nil // creation is scheduled; update does not request a transition
	return campaignObject(body)
}
func (c *Client) validateCampaignReferences(ctx context.Context, body Object) error {
	ids, ok := campaignIDs(body["employee_ids"])
	if !ok {
		return &Error{Code: 2, Stage: "campaign_employees_required"}
	}
	v, e := c.Autodial(ctx, "employees", "", nil)
	if e != nil {
		return e
	}
	available := map[string]bool{}
	for _, g := range v.([]interface{}) {
		o, ok := g.(map[string]interface{})
		if !ok {
			return &Error{Code: 7, Stage: "campaign_employee_schema"}
		}
		emps, ok := o["employees"].([]interface{})
		if !ok {
			return &Error{Code: 7, Stage: "campaign_employee_schema"}
		}
		for _, x := range emps {
			if o, ok := x.(map[string]interface{}); ok {
				available[ID(o)] = true
			}
		}
	}
	for _, id := range ids {
		if !available[id] {
			return &Error{Code: 2, Stage: "campaign_employee_unavailable"}
		}
	}
	action, key := "caller-numbers", "number_capacity_id"
	if body["number_capacity_usage_rule"] == "random" {
		action, key = "caller-groups", "number_capacity_group_id"
	}
	if body["number_capacity_usage_rule"] == "carousel" {
		action, key = "caller-lists", "pure_ani_phone_list_id"
	}
	v, e = c.Autodial(ctx, action, "", nil)
	if e != nil {
		return e
	}
	found := false
	for _, x := range v.([]interface{}) {
		if o, ok := x.(map[string]interface{}); ok && ID(o) == campaignID(body[key]) {
			found = true
		}
	}
	if !found {
		return &Error{Code: 2, Stage: "campaign_caller_unavailable"}
	}
	phones, ok := campaignPhones(body["phone_list"])
	if !ok {
		return &Error{Code: 2, Stage: "campaign_phone_list"}
	}
	v, e = c.Autodial(ctx, "validate", "", Object{"numbers": phones})
	if e != nil {
		return e
	}
	o := v.(map[string]interface{})
	rows, ok := o["numbers"].([]interface{})
	if !ok || len(rows) != len(phones) {
		return &Error{Code: 2, Stage: "campaign_phone_validation"}
	}
	clean := []Object{}
	for _, v := range rows {
		p, ok := v.(map[string]interface{})
		if !ok || (p["validation_error"] != nil && p["validation_error"] != "") {
			return &Error{Code: 2, Stage: "campaign_phone_validation"}
		}
		clean = append(clean, Object{"numa": p["numa"], "name": p["name"], "comment": p["comment"]})
	}
	normalized := make([]interface{}, len(clean))
	for i, p := range clean {
		normalized[i] = p
	}
	body["phone_list"] = normalized
	return nil
}
func (c *Client) sendCampaignWrite(ctx context.Context, method, id string, body Object) error {
	b, e := json.Marshal(body)
	if e != nil {
		return &Error{Code: 2, Stage: "campaign_json"}
	}
	path := c.SupervisorURL + "/auto_call/campaign"
	if id != "" {
		path += "?" + url.Values{"campaign_id": {id}}.Encode()
	}
	r, e := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+c.Session.CabinetToken)
	r.Header.Set("X-Auth-Token", c.Session.CabinetToken)
	r.Header.Set("X-Auth-Type", "jwt")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://go.uiscom.ru")
	r.Header.Set("Referer", "https://go.uiscom.ru/")
	resp, e := c.HTTP.Do(r)
	if e != nil {
		return &Error{Code: 9, Stage: "campaign_dispatch", CampaignID: id}
	}
	defer resp.Body.Close()
	if _, e := io.Copy(io.Discard, io.LimitReader(resp.Body, 16<<20)); e != nil {
		return &Error{Code: 9, Stage: "campaign_response_read", CampaignID: id}
	}
	switch resp.StatusCode {
	case 200, 204:
		return nil
	case 401, 302, 303, 307, 308:
		return &Error{Code: 3, Stage: "campaign_auth", CampaignID: id}
	case 403:
		return &Error{Code: 4, Stage: "campaign_permission", CampaignID: id}
	case 429:
		return &Error{Code: 8, Stage: "campaign_quota", CampaignID: id}
	}
	code := 6
	if resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode/100 == 2 {
		code = 9
	}
	return &Error{Code: code, Stage: "campaign_http", CampaignID: id}
}
func campaignValueEqual(key string, want, got interface{}, loc *time.Location) bool {
	if key == "phone_list" {
		a, ok := campaignPhones(want)
		b, ok2 := campaignPhones(got)
		if !ok || !ok2 {
			return false
		}
		expected, actual := map[string]bool{}, map[string]bool{}
		actualRows := map[string]Object{}
		for _, p := range a {
			expected[fmt.Sprint(p["numa"])] = true
		}
		for _, p := range b {
			if p["status_mnemonic"] != "deleted" {
				actual[fmt.Sprint(p["numa"])] = true
				actualRows[fmt.Sprint(p["numa"])] = p
			}
		}
		if !reflect.DeepEqual(expected, actual) {
			return false
		}
		for _, p := range a {
			for _, k := range []string{"name", "comment"} {
				if v, ok := p[k]; ok {
					got := actualRows[fmt.Sprint(p["numa"])][k]
					if (v == nil || v == "") && (got == nil || got == "") {
						continue
					}
					if v != got {
						return false
					}
				}
			}
		}
		return true
	}
	if key == "employee_ids" {
		a, ok := campaignIDs(want)
		b, ok2 := campaignIDs(got)
		return ok && ok2 && reflect.DeepEqual(a, b)
	}
	if key == "subscriber_time_from" || key == "subscriber_time_till" {
		a, b := fmt.Sprint(want), fmt.Sprint(got)
		if len(a) == 5 {
			a += ":00"
		}
		if len(b) == 5 {
			b += ":00"
		}
		return a == b
	}
	if key == "begin_date" || key == "end_date" {
		if want == nil || got == nil {
			return want == got
		}
		a, e := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprint(want), loc)
		b, e2 := time.Parse(time.RFC3339, fmt.Sprint(got))
		return e == nil && e2 == nil && a.Equal(b)
	}
	a, e := json.Marshal(want)
	b, e2 := json.Marshal(got)
	return e == nil && e2 == nil && bytes.Equal(a, b)
}
func (c *Client) mutateAutodialCampaign(ctx context.Context, action, id string, input Object) (interface{}, error) {
	if e := ValidateCampaignInput(action, input); e != nil {
		return nil, e
	}
	if c.Session.CabinetToken == "" {
		return nil, &Error{Code: 3, Stage: "cabinet_session_refresh_required"}
	}
	if e := c.preflight(ctx); e != nil {
		return nil, e
	}
	patch, e := campaignObject(input)
	if e != nil {
		return nil, e
	}
	loc, e := c.AutodialTimezone(ctx)
	if e != nil {
		return nil, e
	}
	current := Object{}
	before := map[string]Object{}
	if action == "update" {
		v, e := c.Autodial(ctx, "get", id, nil)
		if e != nil {
			return nil, e
		}
		current = v.(map[string]interface{})
		if current["status"] != "draft" && current["status"] != "planned" && current["status"] != "paused" && current["status"] != "in_process" {
			return nil, &Error{Code: 7, Stage: "campaign_not_editable", CampaignID: id}
		}
		if current["status"] != "draft" {
			for k := range patch {
				if k != "phone_list" && k != "employee_ids" {
					return nil, &Error{Code: 7, Stage: "campaign_setting_readonly_after_creation", CampaignID: id}
				}
			}
		}
	} else {
		before, e = c.campaignMatches(ctx, fmt.Sprint(patch["name"]))
		if e != nil {
			return nil, e
		}
		if len(before) > 0 {
			return nil, &Error{Code: 7, Stage: "campaign_name_already_exists"}
		}
	}
	if action == "create" || current["status"] == "draft" {
		begin := patch["begin_date"]
		if begin == nil {
			begin = current["begin_date"]
			if text, ok := begin.(string); ok {
				if d, e := time.Parse(time.RFC3339, text); e == nil {
					begin = d.In(loc).Format("2006-01-02 15:04:05")
				}
			}
		}
		d, e := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprint(begin), loc)
		if e != nil || !d.After(time.Now()) {
			return nil, &Error{Code: 2, Stage: "campaign_begin_must_be_future"}
		}
	}
	body, e := campaignBody(patch, current, loc)
	if e != nil {
		return nil, e
	}
	if body["end_date"] != nil {
		start, e1 := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprint(body["begin_date"]), loc)
		end, e2 := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprint(body["end_date"]), loc)
		if e1 != nil || e2 != nil || !end.After(start) {
			return nil, &Error{Code: 2, Stage: "campaign_end_before_begin"}
		}
	}
	if body["is_subscriber_message_enabled"] == true {
		p, ok := body["subscriber_playlist"].(map[string]interface{})
		if !ok {
			return nil, &Error{Code: 2, Stage: "campaign_message_required"}
		}
		items, ok := p["items"].([]interface{})
		if !ok || len(items) == 0 {
			return nil, &Error{Code: 2, Stage: "campaign_message_required"}
		}
	}
	if e = c.validateCampaignReferences(ctx, body); e != nil {
		return nil, e
	}
	removed := false
	if action == "update" {
		// PUT adds/edits contacts; explicit RPC deletion is required for removals.
		old, _ := campaignPhones(current["phone_list"])
		desired, _ := campaignPhones(body["phone_list"])
		keep := map[string]bool{}
		for _, p := range desired {
			keep[fmt.Sprint(p["numa"])] = true
		}
		remove := []string{}
		for _, p := range old {
			if p["status_mnemonic"] != "deleted" && !keep[fmt.Sprint(p["numa"])] {
				s := p["status_mnemonic"]
				if s == "successfully_processed" || s == "processed_outside_campaign" {
					return nil, &Error{Code: 7, Stage: "campaign_final_contact_cannot_be_removed", CampaignID: id}
				}
				remove = append(remove, fmt.Sprint(p["numa"]))
			}
		}
		if len(remove) > 0 {
			removed = true
			if _, e := c.AutodialRPC(ctx, "delete.auto_call_campaign_phone_numbers", Object{"campaign_id": json.Number(id), "phone_numbers": remove}); e != nil {
				return nil, &Error{Code: 9, Stage: "campaign_contact_removal", CampaignID: id}
			}
		}
	}
	method := "POST"
	if action == "update" {
		method = "PUT"
	}
	dispatchErr := c.sendCampaignWrite(ctx, method, id, body)
	if dispatchErr != nil {
		pe, ok := dispatchErr.(*Error)
		if !ok || pe.Code != 9 {
			if removed {
				return nil, &Error{Code: 9, Stage: "campaign_partial_update", CampaignID: id}
			}
			return nil, dispatchErr
		}
	}
	if action == "create" {
		after, e := c.campaignMatches(ctx, fmt.Sprint(body["name"]))
		if e != nil {
			return nil, &Error{Code: 9, Stage: "campaign_create_identity"}
		}
		fresh := []string{}
		for n := range after {
			if _, old := before[n]; !old {
				fresh = append(fresh, n)
			}
		}
		if len(fresh) != 1 {
			return nil, &Error{Code: 9, Stage: "campaign_create_identity"}
		}
		id = fresh[0]
	}
	v, e := c.Autodial(ctx, "get", id, nil)
	if e != nil {
		return nil, &Error{Code: 9, Stage: "campaign_readback", CampaignID: id}
	}
	after := v.(map[string]interface{})
	if action == "update" && after["status"] != current["status"] {
		return nil, &Error{Code: 9, Stage: "campaign_status_changed_during_edit", CampaignID: id}
	}
	check := Object{}
	for k := range patch {
		check[k] = body[k]
	}
	if action == "create" {
		check = body
	}
	for k, want := range check {
		if k == "status" {
			continue
		}
		if !campaignValueEqual(k, want, after[k], loc) {
			return nil, &Error{Code: 9, Stage: "campaign_field_not_observed", CampaignID: id}
		}
	}
	return Object{"success": true, "verified": true, "campaign_id": json.Number(id), "campaign": after}, nil
}
