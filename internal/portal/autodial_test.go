package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Expectations are the provider frontend's literal routes/verbs, not derived
// from AutodialPlan. These catch accidentally dispatching writes as reads.
func TestAutodialReadContracts(t *testing.T) {
	for _, tc := range []struct{ action, id, method, path, body string }{
		{"list", "", "POST", "/auto_call/campaign_list", `{"begin_date":"2026-10-01 00:00:00","end_date":"2026-10-02 00:00:00"}`},
		{"get", "123", "GET", "/auto_call/campaign", `{}`},
		{"statuses", "", "GET", "/auto_call/campaign_status", `{}`},
		{"calls", "123", "GET", "/auto_call/campaign/call_stat", `{}`},
		{"subscribers", "123", "GET", "/auto_call/campaign/subscriber_stat", `{}`},
		{"employees", "", "GET", "/auto_call/employees_in_groups", `{}`},
		{"stop-list", "", "GET", "/auto_call/excluded_numa", `{}`},
		{"validate", "", "POST", "/auto_call/campaign/validate_numbers", `{"numbers":[]}`},
		{"export", "", "POST", "/auto_call/get_campaigns_numa_list", `{"campaign_ids":[123],"numa_statuses":[]}`},
	} {
		t.Run(tc.action, func(t *testing.T) {
			n := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				if r.Method != tc.method || r.URL.Path != "/v1/common"+tc.path || r.URL.Query().Get("campaign_id") != tc.id {
					t.Error("request contract mismatch")
				}
				if r.Header.Get("Authorization") != "Bearer fixture-jwt" || r.Header.Get("X-Auth-Token") != "fixture-jwt" || r.Header.Get("X-Auth-Type") != "jwt" || r.Header.Get("Cookie") != "" {
					t.Error("wrong credential surface")
				}
				var got Object
				if json.NewDecoder(r.Body).Decode(&got) != nil {
					t.Error("invalid JSON body")
				}
				want := Object{}
				json.Unmarshal([]byte(tc.body), &want)
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(want)
				if string(gb) != string(wb) {
					t.Error("payload changed")
				}
				switch tc.action {
				case "get":
					io.WriteString(w, `{"id":123,"counter":9007199254740993}`)
				case "calls", "subscribers":
					io.WriteString(w, `{"grid_data":null,"chart_data":{},"name":"fixture","status":"draft"}`)
				case "validate":
					io.WriteString(w, `{"numbers":[]}`)
				default:
					io.WriteString(w, `[]`)
				}
			}))
			defer s.Close()
			c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
			c.SupervisorURL = s.URL + "/v1/common"
			p := Object{}
			json.Unmarshal([]byte(tc.body), &p)
			v, e := c.Autodial(context.Background(), tc.action, tc.id, p)
			if e != nil {
				t.Fatal(e)
			}
			if n != 1 {
				t.Fatal("read count")
			}
			if tc.action == "get" && v.(map[string]interface{})["counter"] != json.Number("9007199254740993") {
				t.Fatal("integer fidelity")
			}
		})
	}
}
func TestAutodialFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		code   int
	}{
		{401, "PRIVATE PROVIDER BODY", 3}, {403, "PRIVATE PROVIDER BODY", 4}, {429, "PRIVATE PROVIDER BODY", 8},
		{200, `{"success":false,"secret":"PRIVATE PROVIDER BODY"}`, 6}, {200, `{"error":{}}`, 6},
		{200, `{"id":123}`, 7}, {200, `null`, 7}, {200, `[] {}`, 7}, {200, `<html>PRIVATE PROVIDER BODY</html>`, 7},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
		c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
		c.SupervisorURL = s.URL
		_, e := c.Autodial(context.Background(), "statuses", "", nil)
		var pe *Error
		if !errors.As(e, &pe) || pe.Code != tc.code || strings.Contains(e.Error(), "PRIVATE") {
			t.Fatalf("unsafe or incorrect error: %v", e)
		}
		s.Close()
	}
	c := New(Session{}, time.Second)
	for _, a := range []string{"listen"} {
		_, e := c.Autodial(context.Background(), a, "123", Object{"numa": "70000000000"})
		var pe *Error
		if !errors.As(e, &pe) || pe.Code != 7 {
			t.Fatalf("write %s did not fail before auth/network", a)
		}
	}
}

func TestAutodialRejectsWrongCampaign(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"id":124}`) }))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	_, e := c.Autodial(context.Background(), "get", "123", nil)
	var pe *Error
	if !errors.As(e, &pe) || pe.Code != 7 {
		t.Fatalf("accepted another campaign: %v", e)
	}
}
