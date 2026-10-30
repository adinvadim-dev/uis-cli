package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// AutodialRequest is an allowlisted, source-observed supervisor operation.
// Campaign writes remain plans until a safe non-starting lifecycle is proven.
type AutodialRequest struct {
	Method string     `json:"method"`
	Path   string     `json:"path"`
	Query  url.Values `json:"query,omitempty"`
	Body   Object     `json:"-"`
	Write  bool       `json:"mutation"`
}

func AutodialPlan(action, id string, payload Object) (AutodialRequest, error) {
	r := AutodialRequest{Method: "GET", Query: url.Values{}, Body: payload}
	switch action {
	case "list":
		r.Method = "POST"
		r.Path = "/auto_call/campaign_list"
	case "get":
		r.Path = "/auto_call/campaign"
	case "statuses":
		r.Path = "/auto_call/campaign_status"
	case "calls":
		r.Path = "/auto_call/campaign/call_stat"
	case "subscribers":
		r.Path = "/auto_call/campaign/subscriber_stat"
	case "employees":
		r.Path = "/auto_call/employees_in_groups"
	case "stop-list":
		r.Path = "/auto_call/excluded_numa"
	case "validate":
		r.Method = "POST"
		r.Path = "/auto_call/campaign/validate_numbers"
	case "export":
		r.Method = "POST"
		r.Path = "/auto_call/get_campaigns_numa_list"
	case "import":
		r.Method = "POST"
		r.Path = "/auto_call/campaign/import_numbers"
	case "listen":
		r.Method = "POST"
		r.Path = "/auto_call/campaign/listen_audio_message"
		r.Write = true
	case "create":
		r.Method = "POST"
		r.Path = "/auto_call/campaign"
		r.Write = true
	case "update":
		r.Method = "PUT"
		r.Path = "/auto_call/campaign"
		r.Write = true
	case "status":
		r.Method = "PATCH"
		r.Path = "/auto_call/campaign"
		r.Write = true
	case "exclude", "include":
		r.Method = "POST"
		if action == "include" {
			r.Method = "DELETE"
		}
		r.Path = "/auto_call/excluded_numa"
		r.Write = true
		numa, ok := payload["numa"].(string)
		if !ok || numa == "" {
			return r, &Error{Code: 2, Stage: "autodial_numa_required"}
		}
		r.Query.Set("numa", numa)
		r.Body = nil
	default:
		return r, &Error{Code: 7, Stage: "autodial_contract_unavailable"}
	}
	switch action {
	case "get", "calls", "subscribers", "update", "status":
		if !validCampaignID(id) {
			return r, &Error{Code: 2, Stage: "autodial_campaign_id"}
		}
		r.Query.Set("campaign_id", id)
	}
	return r, nil
}
func validCampaignID(s string) bool {
	if len(s) == 0 || len(s) > 19 || s[0] == '0' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Autodial dispatches reads once with no refresh, redirect or mutation replay.
func (c *Client) Autodial(ctx context.Context, action, id string, payload Object) (interface{}, error) {
	r, e := AutodialPlan(action, id, payload)
	if e != nil {
		return nil, e
	}
	if r.Write {
		return nil, &Error{Code: 7, Stage: "autodial_write_contract_unverified"}
	}
	if c.Session.CabinetToken == "" {
		return nil, &Error{Code: 3, Stage: "cabinet_session_refresh_required"}
	}
	body, e := json.Marshal(r.Body)
	if e != nil {
		return nil, &Error{Code: 2, Stage: "autodial_payload"}
	}
	contentType := "application/json"
	var reader io.Reader = bytes.NewReader(body)
	if action == "import" {
		var b *bytes.Buffer
		b, contentType, e = autodialImportBody(payload)
		if e != nil {
			return nil, e
		}
		reader = b
	}
	base := c.SupervisorURL
	req, e := http.NewRequestWithContext(ctx, r.Method, base+r.Path, reader)
	if e != nil {
		return nil, e
	}
	req.URL.RawQuery = r.Query.Encode()
	req.Header.Set("Authorization", "Bearer "+c.Session.CabinetToken)
	req.Header.Set("X-Auth-Token", c.Session.CabinetToken)
	req.Header.Set("X-Auth-Type", "jwt")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", "https://go.uiscom.ru")
	req.Header.Set("Referer", "https://go.uiscom.ru/")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return nil, &Error{Code: 6, Stage: "autodial_transport"}
	}
	defer resp.Body.Close()
	code := 6
	switch {
	case resp.StatusCode == 401 || resp.StatusCode/100 == 3:
		code = 3
	case resp.StatusCode == 403:
		code = 4
	case resp.StatusCode == 404:
		code = 5
	case resp.StatusCode == 429:
		code = 8
	}
	if resp.StatusCode != 200 {
		return nil, &Error{Code: code, Stage: "autodial_http"}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if e != nil || len(b) > 16<<20 {
		return nil, &Error{Code: 7, Stage: "autodial_response_size"}
	}
	var v interface{}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&v) != nil || d.Decode(new(interface{})) != io.EOF {
		return nil, &Error{Code: 7, Stage: "autodial_schema"}
	}
	switch o := v.(type) {
	case map[string]interface{}:
		if o["error"] != nil || o["success"] == false {
			return nil, &Error{Code: 6, Stage: "autodial_provider"}
		}
	case []interface{}:
	default:
		return nil, &Error{Code: 7, Stage: "autodial_schema"}
	}
	// Minimum source/live-observed shapes. Empty arrays remain valid; a login
	// page, wrong campaign, or differently shaped payload is contract drift.
	shapeOK := false
	switch action {
	case "list", "statuses", "employees", "stop-list", "export":
		_, shapeOK = v.([]interface{})
	case "get":
		o, ok := v.(map[string]interface{})
		shapeOK = ok && ID(o) == id
	case "calls", "subscribers":
		o, ok := v.(map[string]interface{})
		if ok {
			_, grid := o["grid_data"]
			_, chart := o["chart_data"]
			_, name := o["name"].(string)
			_, status := o["status"].(string)
			shapeOK = grid && chart && name && status
		}
	case "validate", "import":
		o, ok := v.(map[string]interface{})
		if ok {
			_, shapeOK = o["numbers"].([]interface{})
		}
	}
	if !shapeOK {
		return nil, &Error{Code: 7, Stage: "autodial_contract_shape"}
	}
	return v, nil
}
