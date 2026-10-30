package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

// CallQuery is the cabinet report-constructor read contract. Filter field names
// and system-list values come from get.report_filters, not get.calls_report.
type CallQuery struct {
	ReportType string      `json:"report_type"`
	Mode       string      `json:"mode"`
	From       string      `json:"date_from"`
	Till       string      `json:"date_till"`
	Fields     []string    `json:"fields"`
	Limit      int         `json:"limit"`
	Offset     int         `json:"offset"`
	Filter     interface{} `json:"filter,omitempty"`
	Sort       []CallSort  `json:"sort,omitempty"`
}
type CallSort struct {
	Field string `json:"field"`
	Order string `json:"order"`
}
type CallPage struct {
	Data     []Object `json:"data"`
	Metadata struct {
		Total *int64 `json:"total_items"`
	} `json:"metadata"`
}

// cabinetRead only dispatches the two source-observed read protocols. POST here
// is a read; redirects are never followed and failures never mean unknown writes.
func (c *Client) cabinetRead(ctx context.Context, discovery bool, body interface{}) (Object, error) {
	if c.Session.CabinetToken == "" {
		return nil, &Error{Code: 3, Stage: "cabinet_session_refresh_required"}
	}
	base, path := c.ReportURL, "/builder/report"
	if discovery {
		base, path = c.RPCURL, "/v2.0"
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Session.CabinetToken)
	req.Header.Set("X-Auth-Token", c.Session.CabinetToken)
	req.Header.Set("X-Auth-Type", "jwt")
	req.Header.Set("Origin", "https://go.uiscom.ru")
	req.Header.Set("Referer", "https://go.uiscom.ru/")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &Error{Code: 6, Stage: "cabinet_transport"}
	}
	defer resp.Body.Close()
	code := 6
	switch {
	case resp.StatusCode == 401 || resp.StatusCode/100 == 3:
		code = 3
	case resp.StatusCode == 403:
		code = 4
	case resp.StatusCode == 429:
		code = 8
	}
	if resp.StatusCode != 200 {
		return nil, &Error{Code: code, Stage: "cabinet_http"}
	}
	var out Object
	d := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	d.UseNumber()
	if d.Decode(&out) != nil || out == nil {
		return nil, &Error{Code: 7, Stage: "cabinet_schema"}
	}
	if e, ok := out["error"].(map[string]interface{}); ok {
		code = 6
		if data, ok := e["data"].(map[string]interface{}); ok && (data["mnemonic"] == "access_token_invalid" || data["mnemonic"] == "access_token_expired") {
			code = 3
		}
		return nil, &Error{Code: code, Stage: "cabinet_provider"}
	}
	return out, nil
}
func (c *Client) SearchCalls(ctx context.Context, q CallQuery) (CallPage, error) {
	var page CallPage
	if q.ReportType != "calls" || q.Mode != "flat" || q.Limit < 1 || q.Limit > 10000 || q.Offset < 0 {
		return page, &Error{Code: 2, Stage: "call_query"}
	}
	out, err := c.cabinetRead(ctx, false, q)
	if err != nil {
		return page, err
	}
	if _, ok := out["metadata"].(map[string]interface{}); !ok {
		return page, &Error{Code: 7, Stage: "call_page_metadata"}
	}
	b, _ := json.Marshal(out)
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&page) != nil || page.Data == nil || (page.Metadata.Total == nil && len(page.Data) > 0) || (page.Metadata.Total != nil && *page.Metadata.Total < 0) || len(page.Data) > q.Limit {
		return CallPage{}, &Error{Code: 7, Stage: "call_page_schema"}
	}
	for _, row := range page.Data {
		id, ok := row["communication_id"].(json.Number)
		n, parseErr := strconv.ParseInt(string(id), 10, 64)
		if !ok || parseErr != nil || n <= 0 {
			return CallPage{}, &Error{Code: 7, Stage: "call_row_identity"}
		}
	}
	return page, nil
}
func (c *Client) DiscoverCalls(ctx context.Context, method, reportType string) (interface{}, error) {
	switch method {
	case "get.report_filters", "get.report_rapid_filters", "get.custom_filters", "get.reports_list":
	default:
		return nil, &Error{Code: 7, Stage: "cabinet_read_unavailable"}
	}
	p := Object{}
	if method != "get.reports_list" {
		p["report_type"] = reportType
	}
	v, err := c.cabinetRead(ctx, true, Object{"jsonrpc": "2.0", "id": "uis-read", "method": method, "params": p})
	if err != nil {
		return nil, err
	}
	if v["id"] != "uis-read" {
		return nil, &Error{Code: 7, Stage: "cabinet_rpc_identity"}
	}
	result, ok := v["result"].(map[string]interface{})
	if !ok {
		return nil, &Error{Code: 7, Stage: "cabinet_rpc_schema"}
	}
	if _, ok := result["data"].([]interface{}); !ok {
		return nil, &Error{Code: 7, Stage: "cabinet_rpc_rows"}
	}
	return result, nil
}
