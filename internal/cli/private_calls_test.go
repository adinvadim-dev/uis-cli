package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func TestPrivateCallSearchWithoutDataAPIToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("UIS_TOKEN", "")
	account := profile.Account{ExpectedLogin: "fixture@example.invalid"}
	account.CredentialProcess.Command = []string{"/must/not/run"}
	if err := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": account}}); err != nil {
		t.Fatal(err)
	}
	s := portal.Session{Login: account.ExpectedLogin, CabinetToken: "fixture-jwt"}
	os.MkdirAll(profile.State("fixture"), 0700)
	b, _ := json.Marshal(s)
	os.WriteFile(filepath.Join(profile.State("fixture"), "session.json"), b, 0600)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	requests := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "uis-prod-msk-report-constructor-api.uiscom.ru" || r.Header.Get("Authorization") != "Bearer fixture-jwt" {
			t.Fatal("unexpected source/auth")
		}
		var q portal.CallQuery
		json.NewDecoder(r.Body).Decode(&q)

		want := map[string]interface{}{"condition": "and", "filters": []interface{}{
			map[string]interface{}{"field": "contact_phone_number", "operator": "=", "value": "70000000000"},
			map[string]interface{}{"field": "virtual_phone_number", "operator": "=", "value": []interface{}{"70000000001"}},
			map[string]interface{}{"field": "employees", "operator": "=", "value": []interface{}{"Fixture employee"}},
			map[string]interface{}{"field": "call_scenario", "operator": "=", "value": []interface{}{"Fixture scenario"}},
			map[string]interface{}{"field": "communication_id", "operator": "=", "value": float64(123)},
			map[string]interface{}{"field": "call_direction", "operator": "=", "value": []interface{}{"Входящий звонок"}},
			map[string]interface{}{"field": "call_status", "operator": "=", "value": []interface{}{"Потерянный"}},
			map[string]interface{}{"field": "call_is_transfer", "operator": "=", "value": []interface{}{"Нет"}},
		}}
		got := q.Filter.(map[string]interface{})
		if got["condition"] != "and" {
			t.Fatal("filters must be AND")
		}
		predicates := got["filters"].([]interface{})
		if len(predicates) != 8 {
			t.Fatal("unexpected predicate count")
		}
		for _, expected := range want["filters"].([]interface{}) {
			found := false
			for _, actual := range predicates {
				if reflect.DeepEqual(actual, expected) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing exact predicate: %v", expected)
			}
		}

		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"communication_id":123}],"metadata":{"total_items":3}}`))}, nil
	})
	out := capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "calls", "search", "--source", "private", "--from", "2026-10-29 00:00:00", "--till", "2026-10-30 00:00:00", "--id", "123", "--phone", "70000000000", "--virtual-number", "70000000001", "--direction", "in", "--lost", "--transfer=false", "--employee", "Fixture employee", "--scenario", "Fixture scenario", "--offset", "2", "--limit", "1"})
		if err := r.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if requests != 1 || !strings.Contains(out, "private.report_constructor") || !strings.Contains(out, `"total_items":3`) {
		t.Fatal(out)
	}
}
func TestPrivateSearchValidationAndDryRunStayOffline(t *testing.T) {
	t.Setenv("UIS_CONFIG", "/must/not/read.toml")
	for i, args := range [][]string{
		{"--source", "private", "--direction", "sideways"}, {"--source", "private", "--employee-id", "12"}, {"--source", "private", "--sort", "unknown"}, {"--source", "invalid"},
	} {
		r := newRootCmd()
		r.SetArgs(append([]string{"--dry-run", "calls", "search"}, args...))
		err := r.Execute()
		var ee *ExitError
		want := 2
		if i == 1 {
			want = 7
		}
		if !errors.As(err, &ee) || ee.Code != want {
			t.Fatalf("want exit %d, got %v", want, err)
		}
	}
	capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "--dry-run", "calls", "search", "--source", "private", "--phone", "70000000000"})
		if err := r.Execute(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAutoSourceFailsClosedForExistingBadSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", dir)
	profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": {ExpectedLogin: "fixture@example.invalid"}}})
	// A genuinely missing session keeps the original Data API route.
	if c, e := selectCallClient(&globalFlags{}, "auto"); e != nil || c != nil {
		t.Fatalf("missing: %v", e)
	}
	os.MkdirAll(profile.State("fixture"), 0700)
	file := filepath.Join(profile.State("fixture"), "session.json")
	for _, body := range []string{`broken`, `{"login":"other@example.invalid","cabinet_token":"fixture"}`} {
		os.WriteFile(file, []byte(body), 0600)
		if _, err := selectCallClient(&globalFlags{}, "auto"); err == nil {
			t.Fatal("silently switched from broken/mismatched private session")
		}
		if c, err := selectCallClient(&globalFlags{}, "dataapi"); err != nil || c != nil {
			t.Fatal("explicit dataapi consulted private session")
		}
	}
	os.WriteFile(file, []byte(`{"login":"fixture@example.invalid"}`), 0600)
	c, err := selectCallClient(&globalFlags{}, "auto")
	if err != nil || c == nil {
		t.Fatal("legacy bound session should select private")
	}
	_, err = c.SearchCalls(context.Background(), portal.CallQuery{ReportType: "calls", Mode: "flat", Limit: 1})
	var pe *portal.Error
	if !errors.As(err, &pe) || pe.Code != 3 {
		t.Fatal("missing cabinet token should require refresh")
	}
}
func TestPrivateRawFilterPreservesLargeNumericID(t *testing.T) {
	r := newRootCmd()
	cmd, _, err := r.Find([]string{"calls", "search"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Flags().Set("filter-json", `{"field":"communication_id","operator":"=","value":9007199254740993}`)
	q, err := privateCallQuery(cmd, "2026-10-29 00:00:00", "2026-10-30 00:00:00", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(q.Filter)
	if !strings.Contains(string(b), `"value":9007199254740993`) {
		t.Fatalf("ID rounded: %s", b)
	}
}

// Private auth recovery must select the cabinet surface even without a Data API key.
func TestPrivateCallSearchRecoveryGuidance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		session    bool
		token      string
		httpStatus int
		exit       int
	}{
		{"missing_session", false, "", 0, 3},
		{"legacy_session", true, "", 0, 3},
		{"expired_session", true, "fixture-jwt", 401, 3},
		{"permission_denied", true, "fixture-jwt", 403, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
			t.Setenv("XDG_STATE_HOME", dir)
			t.Setenv("UIS_TOKEN", "")
			account := profile.Account{ExpectedLogin: "fixture@example.invalid"}
			account.CredentialProcess.Command = []string{"/must/not/run"}
			if err := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": account}}); err != nil {
				t.Fatal(err)
			}
			if tc.session {
				if err := os.MkdirAll(profile.State("fixture"), 0700); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(portal.Session{Login: account.ExpectedLogin, CabinetToken: tc.token})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(profile.State("fixture"), "session.json"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			requests := 0
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: tc.httpStatus, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("private-provider-body"))}, nil
			})
			var code int
			out := capture(t, func() {
				code = Run([]string{"--json", "calls", "search", "--source", "private", "--limit", "1"})
			})
			if code != tc.exit {
				t.Fatalf("want exit %d, got %d: %s", tc.exit, code, out)
			}
			var envelope struct {
				Error struct {
					Details map[string]interface{} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatal(err)
			}
			hint, hasHint := envelope.Error.Details["recovery"]
			if tc.exit == 3 && hint != "auth refresh --surface private --no-input" {
				t.Fatalf("JSON private auth recovery selects wrong surface: %s", out)
			}
			if tc.exit == 4 && hasHint {
				t.Fatalf("permission denial must not suggest reauthentication: %s", out)
			}
			if strings.Contains(out, "private-provider-body") {
				t.Fatal("provider body exposed")
			}
			wantRequests := 0
			if tc.httpStatus != 0 {
				wantRequests = 1
			}
			if requests != wantRequests {
				t.Fatalf("requests=%d want=%d", requests, wantRequests)
			}
		})
	}
}

func TestPrivateAnalysisPagination(t *testing.T) {
	for _, mode := range []string{"complete", "filtered", "page_limit", "offset", "drift", "duplicate", "short", "unknown_empty"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
			t.Setenv("XDG_STATE_HOME", dir)
			t.Setenv("UIS_TOKEN", "")
			if err := profile.Save(profile.Path(), profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": {ExpectedLogin: "fixture@example.invalid"}}}); err != nil {
				t.Fatal(err)
			}
			os.MkdirAll(profile.State("fixture"), 0700)
			os.WriteFile(filepath.Join(profile.State("fixture"), "session.json"), []byte(`{"login":"fixture@example.invalid","cabinet_token":"fixture"}`), 0600)
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			requests := 0
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				var q portal.CallQuery
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"communication_id", "employees", "call_talk_duration", "call_total_duration", "call_type", "is_mass_auto_call_enabled"} {
					found := false
					for _, actual := range q.Fields {
						found = found || actual == field
					}
					if !found {
						t.Fatalf("analysis evidence field missing: %s", field)
					}
				}
				id, total := q.Offset+1, 2
				if mode == "duplicate" {
					id = 1
				}
				if mode == "drift" && requests == 2 {
					total = 3
				}
				body := portal.Object{"metadata": portal.Object{"total_items": total}, "data": []portal.Object{{"communication_id": id, "call_talk_duration": "00:01:00", "call_type": portal.Object{"value_id": "auto_out_calls"}}}}
				if mode == "short" {
					body["data"] = []portal.Object{}
				}
				if mode == "unknown_empty" {
					body = portal.Object{"metadata": portal.Object{}, "data": []portal.Object{}}
				}
				encoded, _ := json.Marshal(body)
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
			})
			args := []string{"--json", "calls", "search", "--source", "private", "--analysis", "--all", "--limit", "1", "--max-pages", "2", "--from", "2026-11-09 00:00:00", "--till", "2026-11-09 18:00:00"}
			if mode == "page_limit" {
				args = append(args, "--max-pages", "1")
			}
			if mode == "filtered" {
				args = append(args, "--scenario", "Synthetic scenario")
			}
			if mode == "offset" {
				args = append(args, "--offset", "1")
			}
			var result error
			out := capture(t, func() { root := newRootCmd(); root.SetArgs(args); result = root.Execute() })
			if mode == "drift" || mode == "duplicate" || mode == "short" {
				var ee *ExitError
				if !errors.As(result, &ee) || ee.Code != 7 {
					t.Fatalf("unstable population accepted: %v", result)
				}
				return
			}
			if result != nil {
				t.Fatal(result)
			}
			var envelope struct {
				Data struct {
					QueryScope struct {
						Filter struct {
							Field string   `json:"field"`
							Value []string `json:"value"`
						} `json:"filter"`
					} `json:"query_scope"`
					Coverage struct {
						Complete bool   `json:"complete"`
						Returned int    `json:"returned"`
						Next     *int   `json:"next_offset"`
						Reason   string `json:"reason"`
					} `json:"coverage"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(out), &envelope) != nil {
				t.Fatal("bad JSON")
			}
			coverage := envelope.Data.Coverage
			if coverage.Complete != (mode == "complete" || mode == "filtered") {
				t.Fatalf("wrong completeness: %s", out)
			}
			if mode == "complete" && (coverage.Returned != 2 || requests != 2 || coverage.Next != nil) {
				t.Fatal(out)
			}
			if mode == "page_limit" && (coverage.Next == nil || *coverage.Next != 1 || coverage.Reason != "page_limit") {
				t.Fatal(out)
			}
			if mode == "unknown_empty" && coverage.Reason != "provider_count_unavailable" {
				t.Fatal(out)
			}
			if mode == "unknown_empty" && coverage.Next != nil {
				t.Fatal("unknown empty page must not return a non-advancing cursor")
			}
			if mode == "filtered" && (envelope.Data.QueryScope.Filter.Field != "call_scenario" || len(envelope.Data.QueryScope.Filter.Value) != 1 || envelope.Data.QueryScope.Filter.Value[0] != "Synthetic scenario") {
				t.Fatal("query restriction missing from exported scope")
			}
		})
	}
}

func TestAnalysisFlagsValidateBeforeCredentials(t *testing.T) {
	t.Setenv("UIS_CONFIG", "/must/not/read.toml")
	for _, args := range [][]string{
		{"--analysis"}, {"--source", "dataapi", "--analysis"}, {"--source", "private", "--all"},
		{"--source", "private", "--max-pages", "2"}, {"--source", "private", "--all", "--max-pages", "0"},
	} {
		root := newRootCmd()
		root.SetArgs(append([]string{"--dry-run", "calls", "search"}, args...))
		var ee *ExitError
		if err := root.Execute(); !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("expected local usage error, got %v", err)
		}
	}
}
