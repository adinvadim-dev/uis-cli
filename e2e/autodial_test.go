//go:build production

package e2e

import (
	"encoding/json"
	"fmt"
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
// New campaign lifecycles require their own explicit residue/write opt-in.
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
		if v["source"] != "private.supervisor" && v["source"] != "private.cabinet_rpc" && v["source"] != "private.comagic" {
			t.Fatal("wrong provenance")
		}
		return v["data"]
	}
	for _, action := range []string{"statuses", "employees", "stop-list", "caller-rules", "caller-numbers", "caller-groups", "caller-lists", "scenarios"} {
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
	for _, action := range []string{"media", "schedules", "timezone"} {
		t.Run("live/"+action, func(t *testing.T) {
			data := object(t, privateData(t, action))
			if action == "timezone" {
				if _, ok := data["timezone"].(string); !ok {
					t.Fatal("missing account timezone")
				}
			} else if _, ok := data["data"].([]any); !ok {
				t.Fatal("catalog array missing")
			}
		})
	}
	t.Run("live/campaign-lifecycle", func(t *testing.T) { h.autodialCampaignLifecycle(t, account, payloadFile) })
	t.Run("live/owned-cleanup-reconciliation", func(t *testing.T) {
		path := os.Getenv("UIS_E2E_AUTODIAL_OWNED_JOURNAL")
		if path == "" {
			t.Skip("existing owned lifecycle journal required")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal("cannot read owned journal")
		}
		var name, id string
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var row map[string]any
			d := json.NewDecoder(strings.NewReader(line))
			d.UseNumber()
			if d.Decode(&row) != nil {
				t.Fatal("invalid ownership journal")
			}
			if n, ok := row["name"].(string); ok {
				name = n
			}
			if n := portal.ID(map[string]any{"id": row["campaign_id"]}); providerID.MatchString(n) {
				id = n
			}
		}
		if !providerID.MatchString(id) || !strings.HasPrefix(name, "uis-cli e2e test") {
			t.Fatal("ownership marker missing")
		}
		c := object(t, privateData(t, "get", id))
		if c["name"] != name || c["status"] != "cancelled" {
			t.Fatal("owned cancelled campaign mismatch")
		}
		for _, v := range c["phone_list"].([]any) {
			if object(t, v)["status_mnemonic"] != "deleted" {
				t.Fatal("active fixture contact remains")
			}
		}
		// Terminal no-ops: they must confirm current cleanup without another mutation.
		for _, action := range []string{"clear", "cancel"} {
			data := object(t, object(t, h.json(t, 0, "--force", "autodial", action, id))["data"])
			if data["verified"] != true || data["no_op"] != true {
				t.Fatal("cleanup was not a verified no-op")
			}
		}
	})

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

	t.Run("live/import", func(t *testing.T) {
		// Parsing this fixture never persists its number into a campaign or calls it.
		file := filepath.Join(scratch, "uis-cli-test-import.csv")
		if os.WriteFile(file, []byte("numa;name;comment\n70000000000;uis-cli e2e import;do not call\n"), 0600) != nil {
			t.Fatal("cannot stage import CSV")
		}
		v := object(t, privateData(t, "import", "--input-file", file))
		rows, ok := v["numbers"].([]any)
		if !ok || len(rows) != 1 {
			t.Fatal("CSV parser did not return one row")
		}
		if object(t, rows[0])["numa"] != "70000000000" {
			t.Fatal("CSV parser changed fixture number")
		}
	})

	t.Run("live/import-xlsx", func(t *testing.T) {
		file := filepath.Join(scratch, "uis-cli-test-import.xlsx")
		autodialXLSX(t, file)
		v := object(t, privateData(t, "import", "--input-file", file))
		rows, ok := v["numbers"].([]any)
		if !ok || len(rows) != 1 || object(t, rows[0])["numa"] != "70000000000" {
			t.Fatal("XLSX parser failed fixture roundtrip")
		}
	})
	// Every write plan and confirmation guard is exercised in a fresh CLI process without usable
	// credentials, using synthetic IDs/content and never a customer target.
	for _, tc := range []struct {
		action       string
		id           bool
		method, path string
		payload      map[string]any
	}{
		{"create", false, "POST", "/auto_call/campaign", map[string]any{"name": "uis-cli e2e test — do not start", "begin_date": "2099-01-01 10:00:00", "employee_ids": []int{8}, "phone_list": []any{map[string]any{"numa": "70000000000"}}, "number_capacity_usage_rule": "fixed", "number_capacity_id": 7}},
		{"update", true, "PUT", "/auto_call/campaign", map[string]any{"phone_list": []any{}}},
		{"status", true, "POST", "/v2.0", map[string]any{"status": "in_process"}},
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
			if plan["method"] != tc.method || plan["path"] != tc.path || plan["network"] != false || plan["mutation"] != true || plan["live_enabled"] != (tc.action != "listen") {
				t.Fatal("unsafe write plan")
			}
			raw, _ := json.Marshal(plan)
			if strings.Contains(string(raw), "uis-cli-invalid-number") {
				t.Fatal("plan leaked payload values")
			}
			if tc.action == "listen" {
				result, code := h.execute(t, append([]string{"--force"}, args...)...)
				if code != 7 || result.Error.Code != "capability_unavailable" {
					t.Fatal("force bypassed preview-call guard")
				}
			} else {
				result, code := h.execute(t, args...)
				if code != 10 || result.Error.Code != "confirmation_required" {
					t.Fatal("write bypassed confirmation")
				}
			}
		})
	}

	for _, action := range []string{"clear", "start", "pause", "resume", "stop", "cancel", "complete", "schedule"} {
		t.Run("confirmation/"+action, func(t *testing.T) {
			offlineGuard(t)
			args := []string{"autodial", action, "1"}
			plan := object(t, h.json(t, 0, append([]string{"--dry-run"}, args...)...))
			if plan["live_enabled"] != true || plan["source"] != "private.cabinet_rpc" || plan["mutation"] != true {
				t.Fatal("wrong control plan")
			}
			r, code := h.execute(t, args...)
			if code != 10 || r.Error.Code != "confirmation_required" {
				t.Fatal("control bypassed confirmation")
			}
		})
	}

	for _, action := range []string{"exclude", "include"} {
		t.Run("confirmation/"+action, func(t *testing.T) {
			offlineGuard(t)
			args := []string{"autodial", action, "--payload-file", payloadFile(action, map[string]any{"numa": "70000000000"})}
			plan := object(t, h.json(t, 0, append([]string{"--dry-run"}, args...)...))
			if plan["live_enabled"] != true || plan["network"] != false {
				t.Fatal("wrong exclusion plan")
			}
			r, code := h.execute(t, args...)
			if code != 10 || r.Error.Code != "confirmation_required" {
				t.Fatal("missing exclusion confirmation")
			}
		})
	}

	t.Run("live/exclusion-write", func(t *testing.T) {
		if os.Getenv("UIS_E2E_AUTODIAL_EXCLUSION_WRITE") != "1" {
			t.Skip("explicit exclusion-write opt-in required; no live write proof")
		}
		path := os.Getenv("UIS_E2E_AUTODIAL_EXCLUSION_JOURNAL")
		if !filepath.IsAbs(path) {
			t.Fatal("new absolute exclusion journal required")
		}
		const marker = "70000000000"
		numbers := func() (map[string]bool, error) {
			r, code := h.execute(t, "autodial", "stop-list")
			if code != 0 {
				return nil, fmt.Errorf("exclusion read exit=%d category=%s", code, r.Error.Code)
			}
			v, ok := r.Data.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid read envelope")
			}
			rows, ok := v["data"].([]any)
			if !ok {
				return nil, fmt.Errorf("invalid exclusion set")
			}
			set := map[string]bool{}
			for _, row := range rows {
				o, ok := row.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid exclusion row")
				}
				numa, ok := o["numa"].(string)
				if !ok {
					return nil, fmt.Errorf("invalid exclusion identity")
				}
				set[numa] = true
			}
			return set, nil
		}
		before, e := numbers()
		if e != nil {
			t.Fatal(e)
		}
		if before[marker] {
			t.Fatal("fixture already excluded; leave unchanged")
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal("journal exists or cannot be claimed; reconcile before rerunning")
		}
		defer f.Close()
		stage := func(s string) error {
			b, _ := json.Marshal(map[string]string{"stage": s, "account": account, "fixture": marker})
			if _, e := f.Write(append(b, '\n')); e != nil {
				return fmt.Errorf("cannot persist recovery stage")
			}
			if f.Sync() != nil {
				return fmt.Errorf("cannot sync recovery stage")
			}
			return nil
		}
		file := payloadFile("exclude-live", map[string]any{"numa": marker})
		write := func(action string) error {
			r, code := h.execute(t, "--force", "autodial", action, "--payload-file", file)
			if code != 0 {
				return fmt.Errorf("exclusion write exit=%d category=%s", code, r.Error.Code)
			}
			v, ok := r.Data.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid write envelope")
			}
			data, ok := v["data"].(map[string]any)
			if !ok || data["verified"] != true {
				return fmt.Errorf("write not verified")
			}
			return nil
		}
		if e := exclusionLifecycle(marker, before, numbers, write, stage); e != nil {
			t.Fatal(e)
		}
	})
	for _, action := range []string{"delete"} {
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
				privateArgs := append(append([]string{}, args...), "--source", "private")
				privatePlan := object(t, h.json(t, 0, append([]string{"--dry-run"}, privateArgs...)...))
				if privatePlan["method"] != tc.method || privatePlan["source"] != "private.cabinet_rpc" || privatePlan["network"] != false {
					t.Fatal("wrong keyless private plan")
				}
				if tc.write {
					r, code := h.execute(t, privateArgs...)
					if code != 10 || r.Error.Code != "confirmation_required" {
						t.Fatal("private RPC mutation bypassed confirmation")
					}
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

	t.Run("official/live-list-keyless", func(t *testing.T) {
		// Keep the bound cabinet session, but make API-key resolution impossible.
		t.Setenv("UIS_CONFIG", guardConfig)
		t.Setenv("UIS_TOKEN", "")
		for _, source := range []string{"auto", "private"} {
			for _, route := range [][]string{{"auto-call-campaigns", "list"}, {"call", "get.auto_call_campaigns"}} {
				args := append(append([]string{}, route...), "--source", source, "--param", "limit:=1")
				v := object(t, h.json(t, 0, args...))
				if v["source"] != "private.cabinet_rpc" {
					t.Fatal("keyless read selected wrong source")
				}
				rows, ok := v["data"].([]any)
				if !ok {
					t.Fatal("missing campaign array")
				}
				for _, row := range rows {
					if !providerID.MatchString(portal.ID(object(t, row))) {
						t.Fatal("invalid campaign ID")
					}
				}
			}
		}
	})
}
