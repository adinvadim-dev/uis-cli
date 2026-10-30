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
