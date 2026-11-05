package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type staffNetworkTrap struct{ calls int }

func (s *staffNetworkTrap) RoundTrip(*http.Request) (*http.Response, error) {
	s.calls++
	return nil, errors.New("staff dry-run attempted network")
}

// Dry-run is a local plan even when profile configuration cannot be read.
// All routes are exercised so a newly added read cannot accidentally start auth.
func TestStaffDryRunEveryRouteHasNoSessionOrCredentialIO(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "invalid.toml")
	state := filepath.Join(dir, "state")
	if err := os.WriteFile(cfg, []byte("invalid TOML ["), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UIS_CONFIG", cfg)
	t.Setenv("UIS_ACCOUNT", "fixture")
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("UIS_TOKEN", "SECRET_TOKEN")
	trap := &staffNetworkTrap{}
	original := http.DefaultTransport
	http.DefaultTransport = trap
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tc := range []struct {
		args               []string
		body, method, path string
	}{
		{[]string{"employees", "list"}, "", "GET", "/common/employee-staff"},
		{[]string{"employees", "get", "11"}, "", "GET", "/common/employee-staff/11"},
		{[]string{"employees", "groups", "11"}, "", "GET", "/common/employee-staff/11"},
		{[]string{"employees", "update", "11"}, `{"last_name":"PRIVATE_PERSON","email":"private@example.invalid"}`, "PUT", "/common/employee-staff/11"},
		{[]string{"groups", "list"}, "", "GET", "/common/employee-group"},
		{[]string{"groups", "get", "21"}, "", "GET", "/common/employee-group/21"},
		{[]string{"softphone", "get", "11"}, "", "GET", "/common/employee-staff/11"},
		{[]string{"softphone", "update", "11"}, `{"is_need_hide_numbers":true,"is_click_to_call_auto_answer_enabled":false,"is_out_calls_auto_answer_enabled":true}`, "PUT", "/common/employee-staff/11"},
		{[]string{"sip-lines", "list", "--employee-id", "11"}, "", "GET", "/pbx/employee-sip-line"},
		{[]string{"sip-lines", "get", "31"}, "", "GET", "/pbx/employee-sip-line/31"},
		{[]string{"sip-lines", "update", "31"}, `{"state":"manual_lock","channels_count":2,"is_multiterminal":false}`, "PUT", "/pbx/employee-sip-line/31"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append([]string{"--json", "--dry-run", "--no-input", "staff"}, tc.args...)
			if tc.body != "" {
				payload := filepath.Join(t.TempDir(), "patch.json")
				if err := os.WriteFile(payload, []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--payload-file", payload)
			}
			out := capture(t, func() {
				if code := Run(args); code != 0 {
					t.Fatalf("dry-run failed %d", code)
				}
			})
			for _, secret := range []string{"SECRET_TOKEN", "PRIVATE_PERSON", "private@example.invalid"} {
				if strings.Contains(out, secret) {
					t.Errorf("dry-run echoed %s", secret)
				}
			}
			var doc struct {
				OK            bool   `json:"ok"`
				SchemaVersion string `json:"schemaVersion"`
				Data          struct {
					Method, Path      string
					Network, Mutation bool
					PayloadFields     []string `json:"payload_fields"`
				} `json:"data"`
			}
			// Read-back intent must be explicit for every write plan.
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatal(err)
			}
			if !doc.OK || doc.SchemaVersion != "uis.cli/v1" || doc.Data.Method != tc.method || doc.Data.Path != tc.path || doc.Data.Network || doc.Data.Mutation != (tc.method == "PUT") {
				t.Fatalf("incorrect safe plan: %s", out)
			}
			if tc.method == "PUT" && !strings.Contains(out, `"read_back":true`) {
				t.Fatal("write plan omitted reconciliation")
			}
		})
	}
	if trap.calls != 0 {
		t.Fatal("dry-run used network")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("dry-run touched state directory")
	}
}

func TestStaffWritesRequireConfirmationBeforeConfig(t *testing.T) {
	t.Setenv("UIS_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	for _, tc := range []struct{ resource, body string }{
		{"employees", `{"last_name":"PRIVATE_PERSON"}`}, {"softphone", `{"is_need_hide_numbers":true}`}, {"sip-lines", `{"state":"active","channels_count":1}`},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			payload := filepath.Join(t.TempDir(), "patch.json")
			if err := os.WriteFile(payload, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			var result int
			out := capture(t, func() {
				result = Run([]string{"--json", "--no-input", "staff", tc.resource, "update", "11", "--payload-file", payload})
			})
			if result != 10 || !strings.Contains(out, `"code":"confirmation_required"`) {
				t.Fatalf("expected confirmation guard before config: %d %s", result, out)
			}
			if strings.Contains(out, "PRIVATE_PERSON") {
				t.Fatal("confirmation failure leaked patch")
			}
		})
	}
}

func TestStaffRejectsMalformedPayloadWithoutAuth(t *testing.T) {
	t.Setenv("UIS_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	for _, body := range []string{`null`, `[]`, `{} {}`, `{"last_name":`, `{}`, `{"password":"SENSITIVE_PASSWORD"}`, `{"va_settings":{"password":"SENSITIVE_PASSWORD"}}`, strings.Repeat(" ", (1<<20)+1)} {
		payload := filepath.Join(t.TempDir(), "patch.json")
		if err := os.WriteFile(payload, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var result int
		out := capture(t, func() {
			result = Run([]string{"--json", "--dry-run", "staff", "employees", "update", "11", "--payload-file", payload})
		})
		if result != 2 || !strings.Contains(out, `"code":"invalid_usage"`) {
			t.Fatalf("malformed/unsafe patch did not fail locally: %d", result)
		}
		if strings.Contains(out, "SENSITIVE_PASSWORD") {
			t.Fatal("invalid payload echoed secret")
		}
	}
}

func TestStaffIDsAndSIPFilterValidateLocally(t *testing.T) {
	t.Setenv("UIS_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	for _, args := range [][]string{{"employees", "get", "../11"}, {"groups", "get", "0"}, {"softphone", "get", "--", "-1"}, {"sip-lines", "get", "1/2"}, {"sip-lines", "list", "--employee-id", "0"}} {
		var result int
		capture(t, func() { result = Run(append([]string{"--json", "--dry-run", "staff"}, args...)) })
		if result != 2 {
			t.Fatalf("invalid target accepted: %v, code %d", args, result)
		}
	}
}
