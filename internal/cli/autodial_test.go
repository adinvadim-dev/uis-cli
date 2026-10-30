package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutodialPlansAndWriteGuard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "missing.toml"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "missing-state"))
	payload := filepath.Join(dir, "request.json")
	os.WriteFile(payload, []byte(`{"name":"PRIVATE_CONTENT","numa":"70000000000","status":"in_process"}`), 0600)
	for _, action := range []string{"listen"} {
		args := []string{"autodial", action}
		if action == "update" || action == "status" {
			args = append(args, "123")
		}
		args = append(args, "--payload-file", payload)
		out := capture(t, func() {
			r := newRootCmd()
			r.SetArgs(append([]string{"--json", "--dry-run"}, args...))
			if e := r.Execute(); e != nil {
				t.Fatal(e)
			}
		})
		if strings.Contains(out, "PRIVATE_CONTENT") || strings.Contains(out, "70000000000") || !strings.Contains(out, `"live_enabled":false`) {
			t.Fatal("unsafe plan")
		}
		r := newRootCmd()
		r.SetArgs(append([]string{"--force", "--no-input"}, args...))
		e := r.Execute()
		var ee *ExitError
		if !errors.As(e, &ee) || ee.Code != 7 {
			t.Fatalf("force bypassed guard: %v", e)
		}
	}
	if _, e := os.Stat(filepath.Join(dir, "missing-state")); !os.IsNotExist(e) {
		t.Fatal("plans touched state")
	}
}
func TestAutodialPayloadValidation(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{} {}`, `{"begin_date":"tomorrow","end_date":"yesterday"}`, `{"begin_date":"2026-10-02 00:00:00","end_date":"2026-10-01 00:00:00"}`} {
		file := filepath.Join(t.TempDir(), "payload.json")
		os.WriteFile(file, []byte(body), 0600)
		r := newRootCmd()
		r.SetArgs([]string{"--dry-run", "autodial", "list", "--payload-file", file})
		e := r.Execute()
		var ee *ExitError
		if !errors.As(e, &ee) || ee.Code != 2 {
			t.Fatalf("accepted malformed payload: %v", e)
		}
	}
}

func TestAutodialImportDryRunDoesNotReadFileOrSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "missing.toml"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "missing-state"))
	out := capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "--dry-run", "autodial", "import", "--input-file", "/must-not-read.csv"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	if strings.Contains(out, "must-not-read.csv") || !strings.Contains(out, `"path":"/auto_call/campaign/import_numbers"`) {
		t.Fatal("unsafe import plan")
	}
	if _, e := os.Stat(filepath.Join(dir, "missing-state")); !os.IsNotExist(e) {
		t.Fatal("dry-run touched session state")
	}
}

func TestAutodialEnabledPlansRequireConfirmation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "missing"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	for _, tc := range []struct {
		action, body string
		id           bool
	}{
		{"create", `{"name":"PRIVATE_CONTENT","begin_date":"2099-01-01 10:00:00","employee_ids":[8],"phone_list":[{"numa":"70000000000"}],"number_capacity_usage_rule":"fixed","number_capacity_id":7}`, false},
		{"update", `{"employee_ids":[8]}`, true}, {"status", `{"status":"in_process"}`, true}, {"clear", `{}`, true}, {"start", ``, true}, {"pause", ``, true}, {"resume", ``, true}, {"stop", ``, true}, {"cancel", ``, true}, {"complete", ``, true}, {"schedule", ``, true},
	} {
		t.Run(tc.action, func(t *testing.T) {
			args := []string{"autodial", tc.action}
			if tc.id {
				args = append(args, "123")
			}
			if tc.body != "" {
				f := filepath.Join(dir, tc.action+".json")
				os.WriteFile(f, []byte(tc.body), 0600)
				args = append(args, "--payload-file", f)
			}
			out := capture(t, func() {
				r := newRootCmd()
				r.SetArgs(append([]string{"--json", "--dry-run"}, args...))
				if e := r.Execute(); e != nil {
					t.Fatal(e)
				}
			})
			if !strings.Contains(out, `"live_enabled":true`) || strings.Contains(out, "PRIVATE_CONTENT") || strings.Contains(out, "70000000000") {
				t.Fatal("unsafe enabled plan")
			}
			r := newRootCmd()
			r.SetArgs(append([]string{"--no-input"}, args...))
			var ee *ExitError
			if !errors.As(r.Execute(), &ee) || ee.Code != 10 {
				t.Fatal("write must require confirmation before config access")
			}
		})
	}
	if _, e := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(e) {
		t.Fatal("dry run touched session")
	}
}

func TestCampaignIDJSONPrecision(t *testing.T) {
	for _, tc := range []struct {
		params    []string
		raw, file string
	}{
		{raw: `{"campaign_id":9007199254740993}`}, {params: []string{"campaign_id:=9007199254740993"}}, {file: `{"campaign_id":9007199254740993}`},
	} {
		f := ""
		if tc.file != "" {
			f = filepath.Join(t.TempDir(), "params.json")
			os.WriteFile(f, []byte(tc.file), 0600)
		}
		v, e := parseParams(tc.params, tc.raw, f)
		if e != nil || v["campaign_id"] != json.Number("9007199254740993") {
			t.Fatal("provider ID rounded")
		}
	}
	v, e := parseHumanFlagValue(GenParam{Type: "array"}, `[9007199254740993]`)
	if e != nil || v.([]any)[0] != json.Number("9007199254740993") {
		t.Fatal("array ID rounded")
	}
}
