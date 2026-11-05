package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Staff operations use the cabinet's current-state API, never the Data API.
// Raw DTOs (including passwords and SIP credentials) stay inside this adapter.
var staffPaths = map[string]string{"employees": "/common/employee-staff", "groups": "/common/employee-group", "sip-lines": "/pbx/employee-sip-line"}
var softphoneFields = strings.Fields("is_need_hide_numbers is_click_to_call_auto_answer_enabled is_out_calls_auto_answer_enabled")
var sipFields = strings.Fields("state channels_count is_multiterminal")
var employeeFields = strings.Fields("first_name last_name description email phone schedule_id staff_group_ids main_staff_group_id")

func staffError(code int, stage string) error { return &Error{Code: code, Stage: "staff_" + stage} }
func staffID(id string) bool {
	n, e := strconv.ParseInt(id, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == id
}
func containsField(fields []string, key string) bool {
	for _, f := range fields {
		if f == key {
			return true
		}
	}
	return false
}

// ValidateStaffPatch is shared by dry-run and dispatch, before any session I/O.
func ValidateStaffPatch(resource string, patch Object) error {
	fields := employeeFields
	if resource == "softphone" {
		fields = softphoneFields
	} else if resource == "sip-lines" {
		fields = sipFields
	} else if resource != "employees" {
		return staffError(7, "write_unavailable")
	}
	if len(patch) == 0 {
		return staffError(2, "empty_patch")
	}
	for k, v := range patch {
		if !containsField(fields, k) {
			return staffError(2, "unsupported_field")
		}
		switch k {
		case "first_name", "last_name", "description", "email", "phone":
			s, ok := v.(string)
			if !ok || len(s) > 4096 || (k == "description" && len([]rune(s)) > 500) || (k == "last_name" && strings.TrimSpace(s) == "") {
				return staffError(2, "text")
			}
		case "state":
			if v != "active" && v != "manual_lock" {
				return staffError(2, "sip_state")
			}
		case "channels_count":
			if _, ok := v.(json.Number); !ok {
				return staffError(2, "channels")
			}
			if staffScalarID(v) != "1" && staffScalarID(v) != "2" {
				return staffError(2, "channels")
			}
		case "schedule_id", "main_staff_group_id":
			if v != nil {
				if _, ok := v.(json.Number); !ok {
					return staffError(2, "reference")
				}
			}
			if v != nil && !staffID(staffScalarID(v)) {
				return staffError(2, "reference")
			}
		case "staff_group_ids":
			a, ok := v.([]interface{})
			if !ok {
				return staffError(2, "groups")
			}
			seen := map[string]bool{}
			for _, x := range a {
				id := staffScalarID(x)
				if _, ok := x.(json.Number); !ok {
					return staffError(2, "groups")
				}
				if !staffID(id) || seen[id] {
					return staffError(2, "groups")
				}
				seen[id] = true
			}
		default:
			if _, ok := v.(bool); !ok {
				return staffError(2, "boolean")
			}
		}
	}
	return nil
}
func staffFieldEqual(key string, a, b interface{}) bool {
	if key != "staff_group_ids" {
		return reflect.DeepEqual(a, b)
	}
	ids := func(v interface{}) ([]string, bool) {
		if v == nil {
			return []string{}, true
		}
		rows, ok := v.([]interface{})
		if !ok {
			return nil, false
		}
		out := []string{}
		for _, r := range rows {
			id := staffScalarID(r)
			if !staffID(id) {
				return nil, false
			}
			out = append(out, id)
		}
		sort.Strings(out)
		return out, true
	}
	x, ok := ids(a)
	y, ok2 := ids(b)
	return ok && ok2 && reflect.DeepEqual(x, y)
}

func staffScalarID(v interface{}) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case string:
		return n
	}
	return ""
}

func validateStaffRecord(resource string, o Object) error {
	if !staffID(ID(o)) {
		return staffError(7, "identity")
	}
	switch resource {
	case "employees":
		if _, ok := o["full_name"].(string); !ok {
			return staffError(7, "employee_schema")
		}
		va, ok := o["va_settings"].(map[string]interface{})
		if !ok {
			return staffError(7, "softphone_schema")
		}
		for _, k := range softphoneFields {
			v, exists := va[k]
			if !exists {
				return staffError(7, "softphone_schema")
			}
			if v != nil {
				if _, ok := v.(bool); !ok {
					return staffError(7, "softphone_schema")
				}
			}
		}
	case "groups":
		if _, ok := o["name"].(string); !ok {
			return staffError(7, "group_schema")
		}
	case "sip-lines":
		if _, ok := o["is_registered"].(bool); !ok {
			return staffError(7, "sip_schema")
		}
		if _, ok := o["state"].(string); !ok {
			return staffError(7, "sip_schema")
		}
	}
	return nil
}

func (c *Client) staffRaw(ctx context.Context, resource, id string) (Object, error) {
	path, ok := staffPaths[resource]
	if !ok || !staffID(id) {
		return nil, staffError(2, "target")
	}
	v, e := c.request(ctx, "GET", path+"/"+id, nil, nil, "", false)
	if e != nil {
		return nil, e
	}
	o, ok := v.(map[string]interface{})
	if !ok || ID(o) != id {
		return nil, staffError(7, "identity")
	}
	if e := validateStaffRecord(resource, o); e != nil {
		return nil, e
	}
	return o, nil
}
func (c *Client) staffListRaw(ctx context.Context, resource string) ([]Object, error) {
	path, ok := staffPaths[resource]
	if !ok {
		return nil, staffError(2, "resource")
	}
	// This REST surface returns the complete array; limit/offset cause HTTP 500.
	v, e := c.request(ctx, "GET", path, nil, nil, "", false)
	if e != nil {
		return nil, e
	}
	rows, e := Rows(v)
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if !staffID(ID(r)) || seen[ID(r)] {
			return nil, staffError(7, "list_identity")
		}
		if e := validateStaffRecord(resource, r); e != nil {
			return nil, e
		}
		seen[ID(r)] = true
	}
	return rows, nil
}

var staffLocator = regexp.MustCompile(`(?i)(sips?:|https?://|@|\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b)`)

func staffSafeText(v interface{}) interface{} {
	if s, ok := v.(string); ok && staffLocator.MatchString(s) {
		return "[redacted]"
	}
	return v
}
func staffProject(resource string, o Object) Object {
	out := Object{}
	fields := strings.Fields("id full_name first_name last_name status_id status_name status_mnemonic is_in_call is_sip_registered sip_line_id sip_channels_count sip_is_multiterminal staff_group_ids staff_group_names main_staff_group_id schedule_id schedule_status employee_state_update_time")
	if resource == "groups" {
		fields = strings.Fields("id name manager_id employee_licenses")
	} else if resource == "sip-lines" {
		fields = strings.Fields("id employee_id employee_full_name state is_registered channels_count is_multiterminal department_ids virtual_phone_state")
	}
	for _, k := range fields {
		if v, ok := o[k]; ok {
			switch x := v.(type) {
			case string:
				out[k] = staffSafeText(x)
			case []interface{}:
				a := make([]interface{}, len(x))
				for i, v := range x {
					switch item := v.(type) {
					case string:
						a[i] = staffSafeText(item)
					case nil, bool, json.Number:
						a[i] = item
					default:
						a[i] = "[redacted]"
					}
				}
				out[k] = a
			case nil, bool, json.Number:
				out[k] = v
			}
		}
	}
	if resource == "employees" {
		if va, ok := o["va_settings"].(map[string]interface{}); ok {
			settings := Object{}
			for _, k := range softphoneFields {
				if v, ok := va[k]; ok {
					if v == nil {
						settings[k] = nil
					} else if b, ok := v.(bool); ok {
						settings[k] = b
					}
				}
			}
			out["softphone"] = settings
		}
	}
	if resource == "sip-lines" {
		if rows, ok := o["sip_line_registration"].([]interface{}); ok {
			a := []Object{}
			for _, r := range rows {
				if reg, ok := r.(map[string]interface{}); ok {
					v := Object{}
					for _, k := range []string{"start_time", "is_closed", "user_agent"} {
						switch x := reg[k].(type) {
						case string:
							v[k] = staffSafeText(x)
						case bool:
							v[k] = x
						}
					}
					a = append(a, v)
				}
			}
			out["registrations"] = a
		}
	}
	return out
}
func staffEnvelope(v interface{}) Object {
	return Object{"source": "private.cabinet", "observed_at": time.Now().UTC().Format(time.RFC3339), "data": v}
}

func (c *Client) StaffList(ctx context.Context, resource, employeeID string) (Object, error) {
	if employeeID != "" && !staffID(employeeID) {
		return nil, staffError(2, "employee_id")
	}
	if employeeID != "" && resource != "sip-lines" {
		return nil, staffError(2, "filter")
	}
	rows, e := c.staffListRaw(ctx, resource)
	if e != nil {
		return nil, e
	}
	out := []Object{}
	for _, r := range rows {
		if employeeID != "" && staffScalarID(r["employee_id"]) != employeeID {
			continue
		}
		out = append(out, staffProject(resource, r))
	}
	return staffEnvelope(out), nil
}
func (c *Client) StaffGet(ctx context.Context, resource, id string) (Object, error) {
	o, e := c.staffRaw(ctx, resource, id)
	if e != nil {
		return nil, e
	}
	projected := staffProject(resource, o)
	if resource == "employees" {
		lines, e := c.staffListRaw(ctx, "sip-lines")
		if e != nil {
			return nil, e
		}
		linked := []Object{}
		for _, line := range lines {
			if staffScalarID(line["employee_id"]) == id {
				linked = append(linked, staffProject("sip-lines", line))
			}
		}
		projected["sip_lines"] = linked
	}
	return staffEnvelope(projected), nil
}
func (c *Client) StaffSoftphone(ctx context.Context, id string) (Object, error) {
	o, e := c.staffRaw(ctx, "employees", id)
	if e != nil {
		return nil, e
	}
	p := staffProject("employees", o)
	settings, ok := p["softphone"]
	if !ok {
		return nil, staffError(7, "softphone_schema")
	}
	return staffEnvelope(Object{"employee_id": id, "settings": settings}), nil
}
func (c *Client) StaffGroups(ctx context.Context, id string) (Object, error) {
	o, e := c.staffRaw(ctx, "employees", id)
	if e != nil {
		return nil, e
	}
	refs := map[string]bool{}
	if v := o["staff_group_ids"]; v != nil {
		a, ok := v.([]interface{})
		if !ok {
			return nil, staffError(7, "group_membership")
		}
		for _, x := range a {
			r := staffScalarID(x)
			if !staffID(r) {
				return nil, staffError(7, "group_membership")
			}
			refs[r] = true
		}
	}
	rows, e := c.staffListRaw(ctx, "groups")
	if e != nil {
		return nil, e
	}
	groups := []Object{}
	for _, r := range rows {
		if refs[ID(r)] {
			groups = append(groups, staffProject("groups", r))
			delete(refs, ID(r))
		}
	}
	if len(refs) > 0 {
		return nil, staffError(7, "group_reference")
	}
	return staffEnvelope(Object{"employee_id": id, "groups": groups}), nil
}

var staffNestedFields = map[string]string{
	"va_settings":                          "is_active forward_rule short_phone phones incremental_dialing_timeout record_talk_direction is_need_hide_numbers coach is_out_calls_auto_answer_enabled is_click_to_call_auto_answer_enabled is_need_out_call_rating sip_line_number_capacity_permission physical_limit_type",
	"phones":                               "id phone is_active forwarding_timeout priority channels_count virtual_phone_usage_rule virtual_phone_id phone_type ext_id",
	"coach":                                "coach_employee_id is_called_always",
	"sip_line_number_capacity_permission":  "is_select is_update is_allowed number_capacity_ids parent_region_ids region_prefixes",
	"omni_settings":                        "is_active consultant_operator_display_name other_employee_chats_access_permission other_chat_employee_ids other_chat_group_ids physical_limit_type is_chat_acceptance_confirmation operator_workplace_access_permission",
	"operator_workplace_access_permission": "is_select is_allowed",
	"ca_settings":                          "is_active physical_limit_type",
	"contact_center_settings":              "is_active physical_limit_type manager_ids",
	"call_center_settings":                 "call_center_manager call_center_employee",
	"call_center_manager":                  "is_active physical_limit_type is_online_monitoring_enabled is_call_queue_management_enabled",
	"call_center_employee":                 "is_active physical_limit_type is_online_monitoring_enabled manager_ids",
	"sip_line_auto_choose_virtual_phone":   "is_active sip_line_virtual_phone_choose_rules",
	"sip_line_virtual_phone_choose_rules":  "id region_id parent_region_ids region_prefixes virtual_phone_id",
	"sip_line_virtual_phone_permission":    "is_active virtual_phone_ids",
}

func staffWritableValue(key string, v interface{}) interface{} {
	fields, known := staffNestedFields[key]
	if !known {
		return v
	}
	if v == nil {
		return nil
	}
	if rows, ok := v.([]interface{}); ok {
		out := []interface{}{}
		for _, r := range rows {
			out = append(out, staffWritableValue(key, r))
		}
		return out
	}
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := Object{}
	for _, f := range strings.Fields(fields) {
		if value, ok := raw[f]; ok {
			out[f] = staffWritableValue(f, value)
		}
	}
	return out
}

// The cabinet submits a full editor form. Keep only observed writable fields;
// never echo computed summaries or auth material into a request DTO.
func staffWriteDTO(resource string, raw Object) Object {
	fields := strings.Fields("id first_name last_name patronymic description email phone schedule_id absence_periods absence_substitute_employee_id staff_group_ids main_staff_group_id va_settings omni_settings ca_settings call_center_settings contact_center_settings")
	if resource == "sip-lines" {
		fields = strings.Fields("id employee_id state channels_count is_multiterminal virtual_phone_usage_rule virtual_phone_id pure_ani_phone_list_id sip_line_auto_choose_virtual_phone sip_line_virtual_phone_permission")
	}
	out := Object{}
	for _, k := range fields {
		if v, ok := raw[k]; ok {
			out[k] = staffWritableValue(k, v)
		}
	}
	if resource == "sip-lines" {
		out["is_send_info_to_mail"] = false
	}
	return out
}

// Compare stable configuration, not live registration/status values. Auth/rights
// omitted from writes must still be unchanged in the fresh read-back.
func staffPreserved(resource string, before, after Object, patch Object) bool {
	fields := strings.Fields("first_name last_name patronymic description email phone schedule_id absence_periods absence_substitute_employee_id staff_group_ids main_staff_group_id login api_permissions is_token is_token_active is_webrtc_token app_role_id data_visibility_group_ids data_visibility_employee_ids data_visibility_site_ids data_visibility_agent_app_ids staff_manager_group_ids va_settings omni_settings ca_settings call_center_settings contact_center_settings")
	if resource == "sip-lines" {
		fields = strings.Fields("state channels_count is_multiterminal employee_id password virtual_phone_usage_rule virtual_phone_id pure_ani_phone_list_id sip_line_auto_choose_virtual_phone sip_line_virtual_phone_permission")
	}
	for _, k := range fields {
		if resource == "softphone" && k == "va_settings" {
			a, _ := before[k].(map[string]interface{})
			b, _ := after[k].(map[string]interface{})
			for key, v := range a {
				if _, changed := patch[key]; !changed && !reflect.DeepEqual(v, b[key]) {
					return false
				}
			}
			continue
		}
		if _, changed := patch[k]; !changed && !staffFieldEqual(k, before[k], after[k]) {
			return false
		}
	}
	return true
}

// StaffUpdate sends once. A response is never proof of a saved effect.
func (c *Client) StaffUpdate(ctx context.Context, resource, id string, patch Object) (Object, error) {
	if !staffID(id) {
		return nil, staffError(2, "target")
	}
	if e := ValidateStaffPatch(resource, patch); e != nil {
		return nil, e
	}
	if e := c.preflight(ctx); e != nil {
		return nil, e
	}
	targetResource := "employees"
	if resource == "sip-lines" {
		targetResource = resource
	}
	before, e := c.staffRaw(ctx, targetResource, id)
	if e != nil {
		return nil, e
	}
	// Unknown fields inside writable blocks cannot be silently dropped: a changed
	// cabinet contract must be researched before writing.
	for k, v := range before {
		if _, known := staffNestedFields[k]; known && !reflect.DeepEqual(v, staffWritableValue(k, v)) {
			return nil, staffError(7, "write_schema")
		}
	}
	originalBytes, _ := json.Marshal(before)
	var original Object
	decoder := json.NewDecoder(bytes.NewReader(originalBytes))
	decoder.UseNumber()
	if decoder.Decode(&original) != nil {
		return nil, staffError(7, "snapshot")
	}
	target := before
	if resource == "softphone" {
		va, ok := before["va_settings"].(map[string]interface{})
		if !ok {
			return nil, staffError(7, "softphone_schema")
		}
		if va["is_active"] != true {
			return nil, staffError(2, "softphone_inactive")
		}
		target = va
	}
	changed := false
	for k, v := range patch {
		if !staffFieldEqual(k, target[k], v) {
			changed = true
		}
	}
	if !changed {
		return staffEnvelope(Object{"id": id, "verified": true, "changed": false}), nil
	}
	// Preserve current writable phone/service blocks; exclude auth and summaries.
	for k, v := range patch {
		target[k] = v
	}

	b, e := json.Marshal(staffWriteDTO(targetResource, before))
	if e != nil {
		return nil, staffError(2, "payload")
	}
	_, dispatchErr := c.request(ctx, "PUT", staffPaths[targetResource]+"/"+url.PathEscape(id), nil, bytes.NewReader(b), "application/json", false)
	if pe, ok := dispatchErr.(*Error); ok && pe.Code != 9 {
		return nil, dispatchErr
	}
	// Reconciliation survives cancellation, is bounded, and never replays the PUT.
	reconcile, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	after, e := c.staffRaw(reconcile, targetResource, id)
	if e == nil {
		saved := after
		if resource == "softphone" {
			saved, _ = after["va_settings"].(map[string]interface{})
		}
		match := true
		for k, v := range patch {
			if !staffFieldEqual(k, saved[k], v) {
				match = false
			}
		}
		if match && staffPreserved(resource, original, after, patch) {
			return staffEnvelope(Object{"id": id, "verified": true, "changed": true}), nil
		}
	}
	return nil, staffError(9, "update_reconciliation")
}

func StaffPlan(resource, action, id string, patch Object) (Object, error) {
	path, ok := staffPaths[resource]
	if resource == "softphone" {
		path = staffPaths["employees"]
		ok = true
	}
	if !ok {
		return nil, staffError(2, "resource")
	}
	write := action == "update"
	switch action {
	case "list":
		if resource == "softphone" {
			return nil, staffError(2, "action")
		}
	case "get", "groups", "update":
		if !staffID(id) {
			return nil, staffError(2, "target")
		}
		path += "/" + id
	default:
		return nil, staffError(2, "action")
	}
	if write {
		if e := ValidateStaffPatch(resource, patch); e != nil {
			return nil, e
		}
	}
	fields := []string{}
	for k := range patch {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	method := "GET"
	if write {
		method = "PUT"
	}
	return Object{"source": "private.cabinet", "method": method, "path": path, "id": id, "mutation": write, "network": false, "payload_fields": fields, "read_back": write}, nil
}
