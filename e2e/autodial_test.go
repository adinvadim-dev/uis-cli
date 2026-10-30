//go:build production

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uis-cli/internal/portal"

	"uis-cli/internal/profile"
)

// Real-binary coverage of every autodial route: live reads and write guards.
// Lifecycle writes stay blocked until non-starting create AND cleanup are proven.
func TestProductionAutodial(t *testing.T) {
	if os.Getenv("UIS_E2E_AUTODIAL") != "1" {
		t.Skip("set UIS_E2E_AUTODIAL=1 to authorize supervisor reads")
	}
	if os.Getenv("UIS_E2E_PRODUCTION") != "1" {
		t.Fatal("UIS_E2E_PRODUCTION=1 required")
	}
	config, account := os.Getenv("UIS_E2E_CONFIG"), os.Getenv("UIS_E2E_ACCOUNT")
	if !filepath.IsAbs(config) || !profile.ValidName.MatchString(account) {
		t.Fatal("explicit config and account required")
	}
	cfg, e := profile.Load(config)
	if e != nil {
		t.Fatal("cannot load profile")
	}
	a, ok := cfg.Accounts[account]
	if !ok || a.ExpectedLogin == "" {
		t.Fatal("bound profile required")
	}
	raw, e := os.ReadFile(filepath.Join(profile.State(account), "session.json"))
	if e != nil {
		t.Fatal("existing session required; refresh separately")
	}
	scratch := t.TempDir()
	local := filepath.Join(scratch, "config.toml")
	if profile.Save(local, profile.Config{DefaultAccount: account, Accounts: map[string]profile.Account{account: a}}) != nil {
		t.Fatal("cannot stage config")
	}
	t.Setenv("UIS_CONFIG", local)
	t.Setenv("UIS_ACCOUNT", account)
	t.Setenv("XDG_STATE_HOME", filepath.Join(scratch, "state"))
	dir := profile.State(account)
	if os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "session.json"), raw, 0600) != nil {
		t.Fatal("cannot stage session")
	}
	binary := filepath.Join(scratch, "uis")
	if exec.Command("go", "build", "-o", binary, "../cmd/uis").Run() != nil {
		t.Fatal("cannot build")
	}
	h := harness{binary: binary}

	// Pin the official host and disable inherited retries/credential surprises.
	t.Setenv("UIS_HOST", "dataapi.uiscom.ru")
	t.Setenv("UIS_API_VERSION", "v2.0")
	payloadFile := func(name string, v any) string {
		t.Helper()
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal("cannot encode fixture")
		}
		p := filepath.Join(scratch, name+".json")
		if os.WriteFile(p, b, 0600) != nil {
			t.Fatal("cannot write private fixture")
		}
		return p
	}
	// Guard checks must remain harmless even if a future implementation starts
	// enabling writes: remove all usable credentials and sessions independently
	// of the guard being tested. Never dispatch a fake ID to a real account.
	guardAccount := profile.Account{ExpectedLogin: a.ExpectedLogin}
	guardAccount.CredentialProcess.Command = []string{"/uis-e2e-must-not-run-credentials"}
	guardConfig := filepath.Join(scratch, "guard-config.toml")
	if profile.Save(guardConfig, profile.Config{DefaultAccount: account, Accounts: map[string]profile.Account{account: guardAccount}}) != nil {
		t.Fatal("cannot stage guard config")
	}
	offlineGuard := func(t *testing.T) {
		t.Helper()
		t.Setenv("UIS_CONFIG", guardConfig)
		t.Setenv("UIS_TOKEN", "")
		t.Setenv("XDG_STATE_HOME", filepath.Join(scratch, "guard-state"))
	}
	privateData := func(t *testing.T, args ...string) any {
		t.Helper()
		args = append([]string{"--timeout", "60s", "autodial"}, args...)
		v := object(t, h.json(t, 0, args...))
		if v["source"] != "private.supervisor" {
			t.Fatal("wrong provenance")
		}
		return v["data"]
	}
	for _, action := range []string{"statuses", "employees", "stop-list"} {
		t.Run("live/"+action, func(t *testing.T) {
			rows, ok := privateData(t, action).([]any)
			if !ok {
				t.Fatal("expected array")
			}
			if action == "statuses" {
				if len(rows) == 0 {
					t.Fatal("empty status catalog")
				}
				for _, row := range rows {
					o := object(t, row)
					if portal.ID(o) == "" {
						t.Fatal("missing status ID")
					}
					if _, ok := o["next_statuses"].([]any); !ok {
						t.Fatal("missing transitions")
					}
				}
			}
		})
	}
	from := os.Getenv("UIS_E2E_AUTODIAL_FROM")
	till := os.Getenv("UIS_E2E_AUTODIAL_TILL")
	if from == "" {
		from = time.Now().AddDate(0, 0, -1).Format("2006-01-02") + " 00:00:00"
	}
	if till == "" {
		till = time.Now().Format("2006-01-02") + " 23:59:59"
	}
	listFile := payloadFile("list", map[string]any{"begin_date": from, "end_date": till, "statuses": []string{}})
	fixture := os.Getenv("UIS_E2E_AUTODIAL_CAMPAIGN_ID")
	if fixture != "" && !providerID.MatchString(fixture) {
		t.Fatal("campaign fixture must be a positive ID")
	}
	t.Run("live/list", func(t *testing.T) {
		rows, ok := privateData(t, "list", "--payload-file", listFile).([]any)
		if !ok {
			t.Fatal("expected campaign array")
		}
		for _, row := range rows {
			if !providerID.MatchString(portal.ID(object(t, row))) {
				t.Fatal("invalid campaign identity")
			}
		}
		if fixture == "" && len(rows) > 0 {
			fixture = portal.ID(object(t, rows[0]))
		}
	})
	for _, action := range []string{"get", "calls", "subscribers", "export"} {
		t.Run("live/"+action, func(t *testing.T) {
			if fixture == "" {
				t.Skip("no campaign fixture: set UIS_E2E_AUTODIAL_CAMPAIGN_ID; dependent read NOT verified")
			}
			if action == "export" {
				file := payloadFile("export", map[string]any{"campaign_ids": []json.Number{json.Number(fixture)}, "numa_statuses": []string{"unsuccessfully_processed", "not_processed"}})
				rows, ok := privateData(t, action, "--payload-file", file).([]any)
				if !ok {
					t.Fatal("expected export array")
				}
				for _, row := range rows {
					o := object(t, row)
					// Cabinet export UI uses these exact columns and two eligible statuses.
					for _, key := range []string{"numa", "campaign_name", "status_name"} {
						if _, ok := o[key].(string); !ok {
							t.Fatal("invalid export row")
						}
					}
				}
				if len(rows) == 0 {
					t.Log("empty export accepted; nonempty export NOT verified")
				} else {
					t.Log("nonempty export object rows verified; contents suppressed")
				}
				return
			}
			v := object(t, privateData(t, action, fixture))
			if action == "get" {
				if portal.ID(v) != fixture {
					t.Fatal("wrong campaign identity")
				}
				if _, ok := v["phone_list"].([]any); !ok {
					t.Fatal("missing campaign contacts")
				}
				if _, ok := v["attempts"].(map[string]any); !ok {
					t.Fatal("missing campaign attempts")
				}
				if _, ok := v["status"].(string); !ok {
					t.Fatal("missing campaign status")
				}
			} else {
				for _, key := range []string{"grid_data", "chart_data", "name", "status"} {
					if _, ok := v[key]; !ok {
						t.Fatal("missing statistics field")
					}
				}
			}
		})
	}
	t.Run("live/validate", func(t *testing.T) {
		file := payloadFile("validate", map[string]any{"numbers": []any{map[string]any{"numa": "uis-cli-invalid-number", "name": "uis-cli validation fixture", "comment": "do not call", "origin": "e2e"}}})
		v := object(t, privateData(t, "validate", "--payload-file", file))
		rows, ok := v["numbers"].([]any)
		if !ok || len(rows) != 1 {
			t.Fatal("validator lost invalid input")
		}
		o := object(t, rows[0])
		if o["validation_error"] == nil || o["validation_error"] == "" {
			t.Fatal("invalid number was accepted")
		}
	})
	// Every blocked write is exercised in a fresh CLI process without usable
	// credentials, using synthetic IDs/content and never a customer target.
	for _, tc := range []struct {
		action       string
		id           bool
		method, path string
		payload      map[string]any
	}{
		{"create", false, "POST", "/auto_call/campaign", map[string]any{"name": "uis-cli e2e test — do not start", "phone_list": []any{}}},
		{"update", true, "PUT", "/auto_call/campaign", map[string]any{"phone_list": []any{}}},
		{"status", true, "PATCH", "/auto_call/campaign", map[string]any{"status": "in_process"}},
		{"exclude", false, "POST", "/auto_call/excluded_numa", map[string]any{"numa": "uis-cli-invalid-number"}},
		{"include", false, "DELETE", "/auto_call/excluded_numa", map[string]any{"numa": "uis-cli-invalid-number"}},
		{"listen", false, "POST", "/auto_call/campaign/listen_audio_message", map[string]any{"numb": "uis-cli-invalid-number", "playlist": []any{}}},
	} {
		t.Run("guard/"+tc.action, func(t *testing.T) {
			offlineGuard(t)
			args := []string{"autodial", tc.action}
			if tc.id {
				args = append(args, "1")
			}
			args = append(args, "--payload-file", payloadFile(tc.action, tc.payload))
			plan := object(t, h.json(t, 0, append([]string{"--dry-run"}, args...)...))
			if plan["method"] != tc.method || plan["path"] != tc.path || plan["network"] != false || plan["mutation"] != true || plan["live_enabled"] != false {
				t.Fatal("unsafe write plan")
			}
			raw, _ := json.Marshal(plan)
			if strings.Contains(string(raw), "uis-cli-invalid-number") {
				t.Fatal("plan leaked payload values")
			}
			result, code := h.execute(t, append([]string{"--force"}, args...)...)
			if code != 7 || result.Error.Code != "capability_unavailable" {
				t.Fatal("force bypassed write guard")
			}
		})
	}
	for _, action := range []string{"import", "delete"} {
		t.Run("blocked/"+action, func(t *testing.T) {
			offlineGuard(t)
			args := []string{"autodial", action}
			if action == "delete" {
				args = append(args, "1")
			}
			for _, flag := range []string{"--dry-run", "--force"} {
				r, code := h.execute(t, append([]string{flag}, args...)...)
				if code != 7 || r.Error.Code != "capability_unavailable" {
					t.Fatal("unsupported contract did not fail closed")
				}
			}
		})
	}
	for _, tc := range []struct {
		route  []string
		method string
		params map[string]any
		write  bool
	}{
		{[]string{"auto-call-campaigns", "list"}, "get.auto_call_campaigns", map[string]any{"limit": 1}, false},
		{[]string{"auto-call-campaign-phone-numbers", "create"}, "create.auto_call_campaign_phone_numbers", map[string]any{"campaign_id": 1, "phone_numbers": []string{}}, true},
		{[]string{"auto-call-campaign-phone-numbers", "delete"}, "delete.auto_call_campaign_phone_numbers", map[string]any{"campaign_id": 1, "phone_numbers": []string{}}, true},
		{[]string{"auto-call-campaign-status", "set"}, "set.auto_call_campaign_status", map[string]any{"id": 1, "status": "in_process"}, true},
	} {
		t.Run("official/"+tc.method, func(t *testing.T) {
			offlineGuard(t)
			raw, _ := json.Marshal(tc.params)
			for _, route := range [][]string{tc.route, {"call", tc.method}} {
				args := append(append([]string{}, route...), "--params-json", string(raw))
				plan := object(t, h.json(t, 0, append([]string{"--dry-run"}, args...)...))
				if plan["method"] != tc.method {
					t.Fatal("wrong official route")
				}
				if tc.write {
					r, code := h.execute(t, args...)
					if code != 10 || r.Error.Code != "confirmation_required" {
						t.Fatal("official mutation bypassed confirmation")
					}
				}
			}
		})
	}
	t.Run("official/live-list", func(t *testing.T) {
		r, code := h.execute(t, "auto-call-campaigns", "list", "--limit", "1")
		if code == 3 && r.Error.Code == "authentication_required" {
			t.Skip("Data API credential unavailable; official live list NOT verified")
		}
		if code != 0 {
			t.Fatalf("official read exit=%d category=%s", code, r.Error.Code)
		}
		v := object(t, r.Data)
		if _, ok := v["data"].([]any); !ok {
			t.Fatal("missing official campaign array")
		}
	})
}
