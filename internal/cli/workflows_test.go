package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uis-cli/internal/client"
	"uis-cli/internal/config"

	"uis-cli/internal/archive"
	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func TestNewDryRunsNeverStartHelperOrSessionIO(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("UIS_TOKEN", "DO_NOT_DISCLOSE")
	cfg := profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": {ExpectedLogin: "fixture@example.invalid"}}}
	a := cfg.Accounts["fixture"]
	a.CredentialProcess.Command = []string{"/must/not/run"}
	cfg.Accounts["fixture"] = a
	if e := profile.Save(profile.Path(), cfg); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"support", "ticket", "create", "--topic-id", "1", "--contact-id", "2", "--phone-id", "3", "--email-id", "4", "--body", "hello", "--attachment", "/must/not/read"}, {"calls", "search", "--phone", "70000000000"}, {"filters", "list", "--report-type", "calls"}, {"auth", "login", "--surface", "private"}, {"support", "sync"}, {"call", "create.communication_comment", "--params-json", `{"communication_id":1,"communication_type":"call","comment":"hello"}`}} {
		out := capture(t, func() {
			r := newRootCmd()
			r.SetArgs(append([]string{"--json", "--dry-run"}, args...))
			if e := r.Execute(); e != nil {
				t.Fatal(e)
			}
		})
		if strings.Contains(out, "DO_NOT_DISCLOSE") {
			t.Fatal("token leaked")
		}
		var doc map[string]interface{}
		if json.Unmarshal([]byte(out), &doc) != nil || doc["schemaVersion"] != "uis.cli/v1" {
			t.Fatalf("bad envelope %s", out)
		}
	}
	if _, e := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(e) {
		t.Fatal("dry-run touched session/archive state")
	}
}
func TestLocalArchiveCommandsDoNotResolveCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	cfg := profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": {}}}
	a := cfg.Accounts["fixture"]
	a.CredentialProcess.Command = []string{"/must/not/run"}
	cfg.Accounts["fixture"] = a
	profile.Save(profile.Path(), cfg)
	s, e := archive.Open(profile.State("fixture"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Page("tickets", "", []portal.Object{{"id": json.Number("123"), "context": "fixture"}}, "1"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	for _, args := range [][]string{{"support", "ticket", "list"}, {"support", "ticket", "show", "123"}, {"support", "status"}} {
		capture(t, func() {
			r := newRootCmd()
			r.SetArgs(append([]string{"--json"}, args...))
			if e := r.Execute(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func capture(t *testing.T, f func()) string {
	t.Helper()
	file, e := os.CreateTemp(t.TempDir(), "stdout")
	if e != nil {
		t.Fatal(e)
	}
	old := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = old; file.Close() }()
	f()
	if _, e = file.Seek(0, 0); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(file.Name())
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestDefaultProfileDoesNotReuseLegacyToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("UIS_CONFIG", "")
	t.Setenv("UIS_TOKEN", "")
	t.Setenv("UIS_ACCOUNT", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	path, e := config.DefaultPath()
	if e != nil {
		t.Fatal(e)
	}
	legacy := config.Default()
	legacy.AccessToken = "wrong-account-fixture"
	if e = config.Save(path, legacy); e != nil {
		t.Fatal(e)
	}
	if e = profile.Save(profile.Path(), profile.Config{DefaultAccount: "team", Accounts: map[string]profile.Account{"team": {}}}); e != nil {
		t.Fatal(e)
	}
	_, cfg, e := loadEffectiveConfig(&globalFlags{})
	if e != nil {
		t.Fatal(e)
	}
	if cfg.AccessToken != "" {
		t.Fatal("legacy credential crossed default profile boundary")
	}
}
func TestDataAPILogoutPreservesPrivateSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "legacy.json"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	cfg := config.Default()
	cfg.AccessToken = "legacy-fixture"
	if e := config.Save(os.Getenv("UIS_CONFIG"), cfg); e != nil {
		t.Fatal(e)
	}
	state := profile.State("default")
	if e := os.MkdirAll(state, 0700); e != nil {
		t.Fatal(e)
	}
	session := filepath.Join(state, "session.json")
	if e := os.WriteFile(session, []byte(`{"login":"fixture"}`), 0600); e != nil {
		t.Fatal(e)
	}
	capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "--force", "auth", "logout", "--surface", "dataapi"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	if _, e := os.Stat(session); e != nil {
		t.Fatal("dataapi logout deleted private session")
	}
	cfg, e := config.Load(os.Getenv("UIS_CONFIG"))
	if e != nil || cfg.AccessToken != "" {
		t.Fatal("local legacy token not removed")
	}
}
func TestRPCErrorExitClasses(t *testing.T) {
	for _, tc := range []struct {
		code        string
		read, write int
	}{{"-32001", 3, 3}, {"-32003", 4, 4}, {"-32029", 8, 8}, {"-32602", 2, 2}, {"-32601", 7, 7}, {"-32008", 7, 7}, {"-32603", 6, 9}, {"-99999", 6, 9}} {
		for _, read := range []bool{true, false} {
			want := tc.write
			if read {
				want = tc.read
			}
			if got := rpcErrorCode(&client.RPCError{Code: json.Number(tc.code)}, read); got != want {
				t.Fatalf("code=%s read=%t got=%d want=%d", tc.code, read, got, want)
			}
		}
	}
}
func TestLiveSearchForwardsQueryWithoutOpeningArchive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	cfg := profile.Config{DefaultAccount: "fixture", Accounts: map[string]profile.Account{"fixture": {ExpectedLogin: "expected"}}}
	if e := profile.Save(profile.Path(), cfg); e != nil {
		t.Fatal(e)
	}
	state := profile.State("fixture")
	os.MkdirAll(state, 0700)
	os.WriteFile(filepath.Join(state, "session.json"), []byte(`{"login":"expected","go_token":"fixture-go"}`), 0600)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("_q") != "router" {
			t.Fatalf("lost positional query: %s", r.URL.RawQuery)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`[{"id":123,"context":"router"}]`))}, nil
	})
	capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "support", "ticket", "search", "router", "--live"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	if _, e := os.Stat(filepath.Join(state, "support.sqlite")); !os.IsNotExist(e) {
		t.Fatal("live read opened writable archive")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRootCommandNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		if seen[c.Name()] {
			t.Fatalf("duplicate command: %s", c.Name())
		}
		seen[c.Name()] = true
	}
}

func TestDryRunRedactionTraversesNestedArrays(t *testing.T) {
	v := map[string]interface{}{"items": []interface{}{map[string]interface{}{"password": "SECRET"}, []interface{}{map[string]interface{}{"access_token": "SECRET"}}}}
	b, e := json.Marshal(redactParams(v))
	if e != nil || strings.Contains(string(b), "SECRET") {
		t.Fatalf("nested secret not redacted: %s %v", b, e)
	}
}

func TestLegacyDataAPILoginAvailability(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(dir, "legacy.json"))
	t.Setenv("UIS_TOKEN", "")
	cfg := config.Default()
	cfg.AccessToken = "fixture"
	if e := config.Save(os.Getenv("UIS_CONFIG"), cfg); e != nil {
		t.Fatal(e)
	}
	capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "auth", "login"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
}
func TestEmployeeFilterUsesCallsArrayContract(t *testing.T) {
	t.Setenv("UIS_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	out := capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "--dry-run", "calls", "search", "--employee-id", "123"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	var envelope struct {
		Data struct {
			Params map[string]interface{} `json:"params"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(out), &envelope); e != nil {
		t.Fatal(e)
	}
	f, ok := envelope.Data.Params["filter"].(map[string]interface{})
	if !ok || f["field"] != "employees" || f["operator"] != "jsquery" || f["value"] != "#.employee_id = 123" {
		t.Fatalf("wrong calls employee filter: %s", out)
	}
}

func TestExplicitLegacyConfigIgnoresUnusedDefaultProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UIS_CONFIG", "")
	t.Setenv("UIS_TOKEN", "")
	t.Setenv("UIS_ACCOUNT", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if e := os.MkdirAll(filepath.Dir(profile.Path()), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(profile.Path(), []byte("INVALID ["), 0600); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "explicit.json")
	c := config.Default()
	c.AccessToken = "explicit-fixture"
	if e := config.Save(path, c); e != nil {
		t.Fatal(e)
	}
	_, got, e := loadEffectiveConfig(&globalFlags{ConfigPath: path})
	if e != nil || got.AccessToken != "explicit-fixture" {
		t.Fatalf("unused profile interfered: %v", e)
	}
}
func TestNamedAccountLogoutPreservesLegacyToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("UIS_CONFIG", "")
	t.Setenv("UIS_TOKEN", "")
	t.Setenv("UIS_ACCOUNT", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	path, e := config.DefaultPath()
	if e != nil {
		t.Fatal(e)
	}
	c := config.Default()
	c.AccessToken = "unrelated-fixture"
	if e = config.Save(path, c); e != nil {
		t.Fatal(e)
	}
	if e = profile.Save(profile.Path(), profile.Config{DefaultAccount: "team", Accounts: map[string]profile.Account{"team": {}}}); e != nil {
		t.Fatal(e)
	}
	capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "--force", "auth", "logout"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	c, e = config.Load(path)
	if e != nil || c.AccessToken != "unrelated-fixture" {
		t.Fatal("named logout deleted unrelated legacy credential")
	}
}
func TestLegTransferFilterContract(t *testing.T) {
	t.Setenv("UIS_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	out := capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"--json", "--dry-run", "calls", "legs", "123", "--transfer"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	if !strings.Contains(out, "is_transfered") || strings.Contains(out, `"is_transfer"`) {
		t.Fatalf("wrong legs transfer field: %s", out)
	}
}

func TestConfigSetPreservesUnrelatedLegacyToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("UIS_CONFIG", "")
	t.Setenv("UIS_TOKEN", "unrelated-env-fixture")
	t.Setenv("UIS_ACCOUNT", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	path, e := config.DefaultPath()
	if e != nil {
		t.Fatal(e)
	}
	c := config.Default()
	c.AccessToken = "preserved-fixture"
	if e = config.Save(path, c); e != nil {
		t.Fatal(e)
	}
	if e = profile.Save(profile.Path(), profile.Config{DefaultAccount: "team", Accounts: map[string]profile.Account{"team": {}}}); e != nil {
		t.Fatal(e)
	}
	capture(t, func() {
		r := newRootCmd()
		r.SetArgs([]string{"config", "set", "--timeout", "45s"})
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	})
	c, e = config.Load(path)
	if e != nil || c.AccessToken != "preserved-fixture" || c.Timeout != "45s" {
		t.Fatalf("unrelated credential changed: timeout=%s err=%v", c.Timeout, e)
	}
}
func TestAnnotationsResolveCommunicationIdentity(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			t.Setenv("UIS_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
			t.Setenv("UIS_TOKEN", "fixture")
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			reads, writes := 0, 0
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var req client.Request
				d := json.NewDecoder(r.Body)
				d.UseNumber()
				if e := d.Decode(&req); e != nil {
					t.Fatal(e)
				}
				var result interface{}
				if req.Method == "get.calls_report" {
					reads++
					row := map[string]interface{}{"id": 123}
					if !missing {
						row["communication_id"] = 456
					}
					result = map[string]interface{}{"data": []interface{}{row}}
				} else {
					writes++
					if req.Method != "create.communication_comment" || fmt.Sprint(req.Params["communication_id"]) != "456" {
						t.Fatalf("wrong annotation identity: %v", req.Params["communication_id"])
					}
					result = map[string]interface{}{"success": "true"}
				}
				b, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result})
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})
			var runErr error
			capture(t, func() {
				r := newRootCmd()
				r.SetArgs([]string{"--json", "--force", "calls", "comment", "123", "--body", "hello"})
				runErr = r.Execute()
			})
			if reads != 1 {
				t.Fatalf("identity reads=%d", reads)
			}
			if missing {
				var e *ExitError
				if !errors.As(runErr, &e) || e.Code != 7 || writes != 0 {
					t.Fatalf("unverified identity dispatched: %v writes=%d", runErr, writes)
				}
			} else if runErr != nil || writes != 1 {
				t.Fatalf("annotation failed: %v writes=%d", runErr, writes)
			}
		})
	}
}
