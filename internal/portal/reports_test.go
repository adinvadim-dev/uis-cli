package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// These fixtures reproduce the live cabinet contract: communication IDs are
// numbers, enum cells are objects, and counts live under metadata.total_items.
func TestCabinetSearchUsesSeparateJWTAndPreservesPage(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/builder/report" || r.Method != "POST" {
			t.Fatal("wrong read endpoint")
		}
		if r.Header.Get("Authorization") != "Bearer fixture-cabinet" || r.Header.Get("X-Auth-Type") != "jwt" || r.Header.Get("Cookie") != "" {
			t.Fatal("wrong credential surface")
		}
		var q CallQuery
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			t.Fatal("query")
		}
		if q.Offset != 2 || q.Limit != 1 || q.From != "2026-10-29 00:00:00" || q.Sort[0].Order != "asc" {
			t.Fatal("lost paging/date/sort")
		}
		fmt.Fprint(w, `{"data":[{"communication_id":9007199254740993,"call_direction":{"value":"Входящий звонок","value_id":"in"}}],"metadata":{"total_items":12}}`)
	})
	c.ReportURL = c.AppURL
	c.Session.CabinetToken = "fixture-cabinet"
	p, err := c.SearchCalls(context.Background(), CallQuery{ReportType: "calls", Mode: "flat", From: "2026-10-29 00:00:00", Limit: 1, Offset: 2, Sort: []CallSort{{Field: "communication_id", Order: "asc"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Data[0]["communication_id"] != json.Number("9007199254740993") || *p.Metadata.Total != 12 {
		t.Fatal("lost identity precision/count")
	}
}
func TestCabinetReadsRejectAuthDriftAndWrongRPCIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		http, code int
	}{
		{"expired", ``, 307, 3}, {"permission", ``, 403, 4}, {"quota", ``, 429, 8},
		{"missing_count", `{"data":[{"communication_id":1}],"metadata":{}}`, 200, 7}, {"missing_id", `{"data":[{}],"metadata":{"total_items":1}}`, 200, 7},
		{"html", `<html>login</html>`, 200, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fake(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.http); fmt.Fprint(w, tc.body) })
			c.ReportURL = c.AppURL
			c.Session.CabinetToken = "fixture"
			_, err := c.SearchCalls(context.Background(), CallQuery{ReportType: "calls", Mode: "flat", Limit: 1})
			pe, ok := err.(*Error)
			if !ok || pe.Code != tc.code {
				t.Fatalf("got %v", err)
			}
		})
	}
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":"other","result":{"data":[]}}`) })
	c.RPCURL = c.AppURL
	c.Session.CabinetToken = "fixture"
	if _, err := c.DiscoverCalls(context.Background(), "get.report_filters", "calls"); err == nil {
		t.Fatal("accepted unrelated response")
	}
	if _, err := c.DiscoverCalls(context.Background(), "set.report_state", "calls"); err == nil {
		t.Fatal("allowed mutation")
	}
}

func TestCabinetEmptyPageMayOmitCount(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[],"metadata":{}}`) })
	c.ReportURL = c.AppURL
	c.Session.CabinetToken = "fixture"
	page, err := c.SearchCalls(context.Background(), CallQuery{ReportType: "calls", Mode: "flat", Limit: 1})
	if err != nil || len(page.Data) != 0 || page.Metadata.Total != nil {
		t.Fatalf("valid empty page rejected: %v", err)
	}
}
func TestCabinetDiscoveryReadContracts(t *testing.T) {
	for _, method := range []string{"get.report_filters", "get.report_rapid_filters", "get.custom_filters", "get.reports_list"} {
		t.Run(method, func(t *testing.T) {
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2.0" || r.Header.Get("Authorization") != "Bearer fixture-cabinet" {
					t.Fatal("wrong metadata source")
				}
				var request struct {
					Method string `json:"method"`
					Params Object `json:"params"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Fatal("invalid rpc")
				}
				if request.Method != method {
					t.Fatal("wrong method")
				}
				if method == "get.reports_list" {
					if len(request.Params) != 0 {
						t.Fatal("reports list must have no params")
					}
				} else if len(request.Params) != 1 || request.Params["report_type"] != "calls" {
					t.Fatal("filter metadata params")
				}
				fmt.Fprint(w, `{"jsonrpc":"2.0","id":"uis-read","result":{"data":[],"metadata":{}}}`)
			})
			c.RPCURL = c.AppURL
			c.Session.CabinetToken = "fixture-cabinet"
			if _, err := c.DiscoverCalls(context.Background(), method, "calls"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
