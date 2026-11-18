package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// recordingHosts are the cabinet media hosts observed in report-constructor
// call_records links. Links on any other host are rejected, never fetched.
var recordingHosts = map[string]bool{"app.uiscom.ru": true, "app.comagic.ru": true}

// MaxRecordingBytes bounds one downloaded recording.
const MaxRecordingBytes = 512 << 20

// CallRecordings resolves recording locators for one verified call. The
// call-detail header supplies identity, start time and talk_file count; the
// report constructor's call_records column supplies the media links. Locators
// are bearer-style URLs: disclose only through the explicit recording link command.
type CallRecordings struct {
	CommunicationID string
	DetailFiles     int
	links           []string
}

// Legs is the number of downloadable recording files.
func (r CallRecordings) Legs() int { return len(r.links) }

func (c *Client) ResolveRecordings(ctx context.Context, id string) (CallRecordings, error) {
	out := CallRecordings{CommunicationID: id}
	v, err := c.Call(ctx, id) // verifies header id == requested id
	if err != nil {
		return out, err
	}
	h := v.(map[string]interface{})["header"].(map[string]interface{})
	if files, ok := h["talk_file"].([]interface{}); ok {
		out.DetailFiles = len(files)
	} else if h["talk_file"] != nil {
		return out, &Error{Code: 7, Stage: "recording_detail_schema"}
	}
	raw, _ := h["start_time"].(string)
	start, perr := time.Parse("2006-01-02T15:04:05", raw)
	if perr != nil {
		return out, &Error{Code: 7, Stage: "recording_start_time"}
	}
	// Both cabinet surfaces report the same wall time (verified live). The report
	// column is slow, so keep the window narrow: ±1 day timed out at 30s live.
	q := CallQuery{ReportType: "calls", Mode: "flat", Limit: 2,
		From: start.Add(-time.Hour).Format("2006-01-02 15:04:05"), Till: start.Add(time.Hour).Format("2006-01-02 15:04:05"),
		Fields: []string{"communication_id", "call_records"},
		Filter: Object{"field": "communication_id", "operator": "=", "value": json.Number(id)}}
	page, err := c.SearchCalls(ctx, q)
	if err != nil {
		return out, err
	}
	var row Object
	for _, r := range page.Data {
		if string(r["communication_id"].(json.Number)) != id { // SearchCalls validated the type
			return out, &Error{Code: 7, Stage: "recording_report_identity"}
		}
		if row != nil {
			return out, &Error{Code: 7, Stage: "recording_report_identity"}
		}
		row = r
	}
	if row == nil {
		return out, &Error{Code: 5, Stage: "recording_report_row"}
	}
	if row["call_records"] == nil {
		return out, nil
	}
	cell, ok := row["call_records"].(map[string]interface{})
	if !ok {
		return out, &Error{Code: 7, Stage: "recording_report_schema"}
	}
	links, ok := cell["links"].([]interface{})
	if !ok && cell["links"] != nil {
		return out, &Error{Code: 7, Stage: "recording_report_schema"}
	}
	for _, l := range links {
		s, ok := l.(string)
		if !ok || !validRecordingLink(s, id) {
			return out, &Error{Code: 7, Stage: "recording_locator_identity"}
		}
		out.links = append(out.links, s)
	}
	return out, nil
}

// validRecordingLink accepts only https://<cabinet media host>/system/media/talk/<id>/<token>/
// whose embedded call ID equals the requested one.
func validRecordingLink(s, id string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || !recordingHosts[u.Host] || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(p) == 5 && p[0] == "system" && p[1] == "media" && p[2] == "talk" && p[3] == id && p[4] != ""
}

// DownloadRecording streams leg (0-based) into w. The media link is
// self-authorizing, so no session credential is attached to the request.
func (c *Client) DownloadRecording(ctx context.Context, r CallRecordings, leg int, w io.Writer) (string, int64, error) {
	if leg < 0 || leg >= len(r.links) {
		return "", 0, &Error{Code: 5, Stage: "recording_leg"}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", r.links[leg], nil)
	if err != nil {
		return "", 0, &Error{Code: 7, Stage: "recording_locator"}
	}
	req.Header.Set("Accept", "audio/*")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, &Error{Code: 6, Stage: "recording_transport"}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return "", 0, &Error{Code: 5, Stage: "recording_media"}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return "", 0, &Error{Code: 4, Stage: "recording_media"}
	case resp.StatusCode == 429:
		return "", 0, &Error{Code: 8, Stage: "recording_media"}
	case resp.StatusCode != 200:
		return "", 0, &Error{Code: 6, Stage: "recording_media"}
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "audio/") {
		return "", 0, &Error{Code: 7, Stage: "recording_content_type"}
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, MaxRecordingBytes+1))
	if err != nil {
		return "", 0, &Error{Code: 6, Stage: "recording_transfer"}
	}
	if n == 0 || n > MaxRecordingBytes {
		return "", 0, &Error{Code: 7, Stage: "recording_size"}
	}
	return ct, n, nil
}

// Link returns a validated single-recording capability for explicit disclosure.
// Callers must verify DownloadRecording succeeds before disclosing it.
func (r CallRecordings) Link(leg int) string {
	if leg < 0 || leg >= len(r.links) {
		return ""
	}
	return r.links[leg]
}
