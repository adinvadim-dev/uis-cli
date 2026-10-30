// Package portal implements the source-observed infopin wire contract. Writes
// never refresh sessions or replay callbacks.
package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Error struct {
	Code            int
	Stage, TicketID string
	CampaignID      string
}

func (e *Error) Error() string {
	if e.CampaignID != "" {
		return fmt.Sprintf("private operation failed at %s (campaign_id=%s)", e.Stage, e.CampaignID)
	}
	return fmt.Sprintf("private operation failed at %s (ticket_id=%s)", e.Stage, e.TicketID)
}

type Object = map[string]interface{}
type Session struct {
	CabinetToken string    `json:"cabinet_token,omitempty"`
	AppToken     string    `json:"app_token"`
	GoToken      string    `json:"go_token"`
	Login        string    `json:"login"`
	CustomerID   string    `json:"customer_id"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func LoadSession(dir string) (Session, error) {
	var s Session
	b, e := os.ReadFile(filepath.Join(dir, "session.json"))
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}

// Promote atomically replaces a session only after the supplied binding verifier
// succeeds. Login uses credential binding plus an authenticated read; this is not
// an independent provider identity assertion and cannot authorize writes.
func Promote(dir string, s Session, expected string, verify func(Session) (string, error)) error {
	principal, e := verify(s)
	if e != nil {
		return e
	}
	if expected == "" || principal != expected {
		return errors.New("principal mismatch or missing binding")
	}
	s.Login = principal
	s.UpdatedAt = time.Now().UTC()
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".staged-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	b, e := json.Marshal(s)
	if e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), filepath.Join(dir, "session.json"))
}

type Client struct {
	BaseURL, AppURL   string
	ReportURL, RPCURL string
	SupervisorURL     string
	HTTP              *http.Client
	Session           Session
	Verify            func(context.Context, Session) error
}

func New(s Session, timeout time.Duration) *Client {
	return &Client{SupervisorURL: "https://uis-prod-msk-comagic-supervisor-workplace-api.uiscom.ru/v1/common", ReportURL: "https://uis-prod-msk-report-constructor-api.uiscom.ru", RPCURL: "https://dataapi.uiscom.ru", BaseURL: "https://uis-prod-msk-comagic-api.uiscom.ru", AppURL: "https://app.uiscom.ru", Session: s, HTTP: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func ID(o Object) string {
	switch v := o["id"].(type) {
	case json.Number:
		return string(v)
	case string:
		if v != "" {
			return v
		}
	}
	return ""
}
func (c *Client) request(ctx context.Context, method, path string, q url.Values, body io.Reader, ct string, app bool) (interface{}, error) {
	base := c.BaseURL
	token := c.Session.GoToken
	if app {
		base = c.AppURL
		token = c.Session.AppToken
	}
	if token == "" {
		return nil, &Error{Code: 3, Stage: "session"}
	}
	req, e := http.NewRequestWithContext(ctx, method, base+path, body)
	if e != nil {
		return nil, e
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if app {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.AddCookie(&http.Cookie{Name: "access_token", Value: token})
		req.Header.Set("Origin", "https://go.uiscom.ru")
		req.Header.Set("Referer", "https://go.uiscom.ru/")
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, e := c.HTTP.Do(req)
	write := method != "GET"
	if e != nil {
		code := 6
		if write {
			code = 9
		}
		return nil, &Error{Code: code, Stage: "dispatch"}
	}
	defer resp.Body.Close()
	code := 0
	switch {
	case resp.StatusCode == 401 || resp.StatusCode/100 == 3:
		code = 3
	case resp.StatusCode == 403:
		code = 4
	case resp.StatusCode == 404:
		code = 5
	case resp.StatusCode == 429:
		code = 8
	case resp.StatusCode >= 500:
		code = 6
		if write {
			code = 9
		}
	case resp.StatusCode >= 400:
		code = 6
	}
	if code != 0 {
		return nil, &Error{Code: code, Stage: "http"}
	}
	var out interface{}
	d := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	d.UseNumber()
	if d.Decode(&out) != nil {
		code = 7
		if write {
			code = 9
		}
		return nil, &Error{Code: code, Stage: "response"}
	}
	if o, ok := out.(map[string]interface{}); ok {
		if o["success"] == false {
			code = 6
			if write {
				code = 9
			}
			return nil, &Error{Code: code, Stage: "provider"}
		}
	}
	return out, nil
}
func (c *Client) Read(ctx context.Context, resource string, q url.Values) (interface{}, error) {
	return c.request(ctx, "GET", "/infopin/"+resource, q, nil, "", false)
}
func Rows(v interface{}) ([]Object, error) {
	if o, ok := v.(map[string]interface{}); ok {
		if d, yes := o["data"]; yes {
			v = d
		} else if ID(o) != "" {
			return []Object{o}, nil
		}
	}
	a, ok := v.([]interface{})
	if !ok {
		return nil, &Error{Code: 7, Stage: "schema"}
	}
	out := make([]Object, 0, len(a))
	for _, x := range a {
		o, ok := x.(map[string]interface{})
		if !ok || ID(o) == "" {
			return nil, &Error{Code: 7, Stage: "identity"}
		}
		out = append(out, o)
	}
	return out, nil
}
func (c *Client) Ticket(ctx context.Context, id string) (Object, error) {
	v, e := c.Read(ctx, "tech-support", url.Values{"id": {id}})
	if e != nil {
		return nil, e
	}
	rows, e := Rows(v)
	if e != nil {
		return nil, e
	}
	for _, r := range rows {
		if ID(r) == id {
			return r, nil
		}
	}
	return nil, &Error{Code: 5, Stage: "ticket"}
}

// Comments loads the full thread for a ticket. Live infopin rejects
// sort/order/offset/limit on this resource (HTTP 500). The full set is
// returned on offset 0; later offsets are empty so archive sync terminates
// without re-fetching. limit is ignored so a page size smaller than the
// thread cannot drop comments.
func (c *Client) Comments(ctx context.Context, t Object, offset, limit int) ([]Object, error) {
	if offset > 0 {
		return nil, nil
	}
	ns, ok := t["is_ticket"].(bool)
	if !ok {
		return nil, &Error{Code: 7, Stage: "namespace"}
	}
	v, e := c.Read(ctx, "tech-support-comment", url.Values{"tech_support_id": {ID(t)}, "is_ticket": {strconv.FormatBool(ns)}})
	if e != nil {
		return nil, e
	}
	return Rows(v)
}

// Tickets loads the account support list. Live infopin rejects
// category/offset/limit query params (HTTP 500); the unfiltered list is the
// working contract. The full set is returned on offset 0; later offsets are
// empty so archive sync terminates. limit is ignored so a sync page size
// smaller than the account list cannot drop tickets.
func (c *Client) Tickets(ctx context.Context, offset, limit int) ([]Object, error) {
	if offset > 0 {
		return nil, nil
	}
	v, e := c.Read(ctx, "tech-support", nil)
	if e != nil {
		return nil, e
	}
	return Rows(v)
}

// validateAttachments checks every path is readable and ≤10 MiB before any
// provider write. Create must call this before the ticket POST so a local
// attachment failure cannot leave an orphan ticket.
func validateAttachments(files []string) error {
	for _, path := range files {
		f, e := os.Open(path)
		if e != nil {
			return &Error{Code: 2, Stage: "attachment_unreadable"}
		}
		st, e := f.Stat()
		f.Close()
		if e != nil || st.IsDir() || st.Size() > 10<<20 {
			return &Error{Code: 2, Stage: "attachment_invalid"}
		}
	}
	return nil
}

func multipartBody(fields Object, files []string) (*bytes.Buffer, string, error) {
	if e := validateAttachments(files); e != nil {
		return nil, "", e
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	raw, e := json.Marshal(fields)
	if e != nil {
		return nil, "", e
	}
	if e = w.WriteField("request", string(raw)); e != nil {
		return nil, "", e
	}
	for _, path := range files {
		f, e := os.Open(path)
		if e != nil {
			return nil, "", &Error{Code: 2, Stage: "attachment_unreadable"}
		}
		st, e := f.Stat()
		if e != nil || st.Size() > 10<<20 {
			f.Close()
			return nil, "", &Error{Code: 2, Stage: "attachment_invalid"}
		}
		p, e := w.CreateFormFile("files", filepath.Base(path))
		if e == nil {
			_, e = io.Copy(p, io.LimitReader(f, 10<<20+1))
		}
		f.Close()
		if e != nil {
			return nil, "", e
		}
	}
	e = w.Close()
	return &b, w.FormDataContentType(), e
}

// ProbeAuthenticatedRead proves the Go session can still perform an
// authenticated support metadata read. Write preflight uses this (or an
// injected Verify) before dispatch. It is credential-binding + live read
// evidence, not a dedicated /me or CSRF token contract.
func (c *Client) ProbeAuthenticatedRead(ctx context.Context) error {
	v, err := c.Read(ctx, "tech-support-web-type", nil)
	if err != nil {
		return err
	}
	_, err = Rows(v)
	return err
}

func (c *Client) preflight(ctx context.Context) error {
	if c.Verify == nil {
		return &Error{Code: 7, Stage: "principal_probe_unavailable"}
	}
	return c.Verify(ctx, c.Session)
}
func (c *Client) Post(ctx context.Context, resource string, fields Object, files []string) (Object, error) {
	b, ct, e := multipartBody(fields, files)
	if e != nil {
		return nil, e
	}
	v, e := c.request(ctx, "POST", "/infopin/"+resource, nil, b, ct, false)
	if e != nil {
		return nil, e
	}
	rows, e := Rows(v)
	if e != nil || len(rows) != 1 {
		return nil, &Error{Code: 9, Stage: "provider_identity"}
	}
	return rows[0], nil
}
func (c *Client) Create(ctx context.Context, fields Object, files []string) (Object, error) {
	if e := c.preflight(ctx); e != nil {
		return nil, e
	}
	if e := validateAttachments(files); e != nil {
		return nil, e
	}
	t, e := c.Post(ctx, "tech-support", fields, nil)
	if e != nil {
		return nil, e
	}
	ns, ok := t["is_ticket"].(bool)
	if !ok {
		return nil, &Error{Code: 9, Stage: "initial_comment_namespace", TicketID: ID(t)}
	}
	_, e = c.Post(ctx, "tech-support-comment", Object{"tech_support_id": t["id"], "is_ticket": ns, "message": fields["context"]}, files)
	if e != nil {
		return nil, &Error{Code: 9, Stage: "initial_comment", TicketID: ID(t)}
	}
	return t, nil
}
func (c *Client) Reply(ctx context.Context, id, message string, files []string) (Object, error) {
	if e := c.preflight(ctx); e != nil {
		return nil, e
	}
	if e := validateAttachments(files); e != nil {
		return nil, e
	}
	t, e := c.Ticket(ctx, id)
	if e != nil {
		return nil, e
	}
	ns, ok := t["is_ticket"].(bool)
	if !ok {
		return nil, &Error{Code: 7, Stage: "namespace"}
	}
	if strings.HasPrefix(fmt.Sprint(t["status"]), "close") || t["status"] == "canceled" {
		return nil, &Error{Code: 2, Stage: "closed_ticket"}
	}
	return c.Post(ctx, "tech-support-comment", Object{"tech_support_id": t["id"], "is_ticket": ns, "message": message}, files)
}
func (c *Client) Status(ctx context.Context, id, status string) (Object, error) {
	if e := c.preflight(ctx); e != nil {
		return nil, e
	}
	t, e := c.Ticket(ctx, id)
	if e != nil {
		return nil, e
	}
	ns, ok := t["is_ticket"].(bool)
	shared := t["multiple_number"] != nil && fmt.Sprint(t["multiple_number"]) != ""
	if !ok || shared {
		return nil, &Error{Code: 7, Stage: "unsupported_ticket"}
	}
	if status == "created" {
		if allowed, ok := t["is_can_reopen"].(bool); ok && !allowed {
			return nil, &Error{Code: 2, Stage: "reopen_not_allowed", TicketID: id}
		}
		if raw, ok := t["date_close"].(string); ok {
			for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
				if d, err := time.Parse(layout, raw); err == nil {
					if time.Since(d) > 14*24*time.Hour {
						return nil, &Error{Code: 2, Stage: "reopen_older_than_14_days", TicketID: id}
					}
					break
				}
			}
		}
	}
	b, _ := json.Marshal(Object{"status": status, "is_ticket": ns})
	_, dispatchErr := c.request(ctx, "PUT", "/infopin/tech-support/"+url.PathEscape(id), nil, bytes.NewReader(b), "application/json", false)
	if pe, ok := dispatchErr.(*Error); ok && pe.Code != 9 {
		return nil, dispatchErr
	}
	after, e := c.Ticket(ctx, id)
	if e == nil {
		got := fmt.Sprint(after["status"])
		if (status == "closed" && (got == "close_customer" || got == "close_operator" || got == "canceled")) || (status == "created" && (got == "created" || got == "waiting_operator")) {
			return after, nil
		}
	}
	return nil, &Error{Code: 9, Stage: "status_reconciliation", TicketID: id}
}
func (c *Client) Call(ctx context.Context, id string) (interface{}, error) {
	v, e := c.request(ctx, "GET", "/analytics/call/call_details_data/", url.Values{"call_id": {id}}, nil, "", true)
	if e != nil {
		return nil, e
	}
	o, ok := v.(map[string]interface{})
	if !ok {
		return nil, &Error{Code: 7, Stage: "call_schema"}
	}
	d, ok := o["data"].(map[string]interface{})
	if !ok {
		return nil, &Error{Code: 7, Stage: "call_schema"}
	}
	h, ok := d["header"].(map[string]interface{})
	if !ok || ID(h) != id {
		return nil, &Error{Code: 7, Stage: "call_identity"}
	}
	if _, ok = d["grid"].([]interface{}); !ok {
		return nil, &Error{Code: 7, Stage: "call_grid"}
	}
	return d, nil
}
