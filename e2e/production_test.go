//go:build production

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
	"uis-cli/internal/spec"
)

// This suite executes the real binary against UIS. No HTTP mocks or write replay.
func TestProduction(t *testing.T) {
	if os.Getenv("UIS_E2E_PRODUCTION") != "1" {
		t.Fatal("set UIS_E2E_PRODUCTION=1 to authorize production reads")
	}
	configPath, account := os.Getenv("UIS_E2E_CONFIG"), os.Getenv("UIS_E2E_ACCOUNT")
	if configPath == "" || !filepath.IsAbs(configPath) || !profile.ValidName.MatchString(account) {
		t.Fatal("UIS_E2E_CONFIG must be absolute and UIS_E2E_ACCOUNT must name an explicit account")
	}
	cfg, err := profile.Load(configPath)
	if err != nil {
		t.Fatal("cannot load E2E profile")
	}
	a, ok := cfg.Accounts[account]
	if !ok || a.ExpectedLogin == "" {
		t.Fatal("E2E account must exist and bind expected_login")
	}
	// Copy only the selected profile; never edit the user's config or archive.
	scratch := t.TempDir()
	localConfig := filepath.Join(scratch, "config.toml")
	if err := profile.Save(localConfig, profile.Config{DefaultAccount: account, Accounts: map[string]profile.Account{account: a}}); err != nil {
		t.Fatal(err)
	}
	sourceSession := filepath.Join(profile.State(account), "session.json")
	t.Setenv("UIS_CONFIG", localConfig)
	t.Setenv("UIS_ACCOUNT", account)
	t.Setenv("XDG_STATE_HOME", filepath.Join(scratch, "state"))
	// Ignore host/version overrides, including ones in an operator's shell.
	t.Setenv("UIS_HOST", "dataapi.uiscom.ru")
	t.Setenv("UIS_API_VERSION", "v2.0")
	t.Setenv("UIS_RETRIES", "0")
	t.Setenv("UIS_TIMEOUT", "30s")
	binary := filepath.Join(scratch, "uis")
	build := exec.Command("go", "build", "-o", binary, "../cmd/uis")
	if err := build.Run(); err != nil {
		t.Fatal("cannot build CLI")
	}
	h := harness{binary: binary}
	if os.Getenv("UIS_E2E_LOGIN") == "1" {
		if !t.Run("live/private-login", func(t *testing.T) { h.json(t, 0, "auth", "login", "--surface", "private") }) {
			t.Fatal("private login failed; stopping dependent checks")
		}
	} else {
		raw, err := os.ReadFile(sourceSession)
		if err != nil {
			t.Fatal("no existing session; use UIS_E2E_LOGIN=1 (may incur captcha charges)")
		}
		dir := profile.State(account)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "session.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		t.Log("private login/refresh NOT tested; using an isolated copy of existing session")
	}

	for _, args := range [][]string{{"account", "list"}, {"account", "show"}, {"account", "doctor"}, {"auth", "status"}, {"capability", "list"}, {"capability", "probe"}} {
		t.Run("local/"+strings.Join(args, "-"), func(t *testing.T) { h.json(t, 0, args...) })
	}
	t.Run("live/dataapi", func(t *testing.T) {
		h.json(t, 0, "auth", "login", "--surface", "dataapi")
		// Unlike auth login, these calls prove provider acceptance of the token.
		h.json(t, 0, "account", "get")
		h.json(t, 0, "call", "get.account")
	})
	for _, args := range [][]string{{"reports", "list", "--source", "dataapi"}, {"filters", "list", "--source", "dataapi", "--report-type", "calls"}, {"filters", "show", "--source", "dataapi", "--report-type", "calls"}, {"filters", "rapid", "--source", "dataapi", "--report-type", "calls"}} {
		t.Run("live/"+strings.Join(args[:2], "-"), func(t *testing.T) { h.json(t, 0, args...) })
	}
	from := time.Now().AddDate(0, 0, -7).Format("2006-01-02 15:04:05")
	till := time.Now().Format("2006-01-02 15:04:05")
	window := []string{"--from", from, "--till", till}
	t.Run("live/calls", func(t *testing.T) {
		rows := mustRows(t, h.json(t, 0, append([]string{"calls", "search", "--source", "dataapi", "--limit", "1"}, window...)...))
		if len(rows) == 0 {
			t.Fatal("no recent call fixture; populate account or extend the test window")
		}
		id := mustID(t, rows[0])
		filtered := mustRows(t, h.json(t, 0, append([]string{"calls", "search", "--source", "dataapi", "--id", id, "--limit", "1"}, window...)...))
		if len(filtered) != 1 || mustID(t, filtered[0]) != id {
			t.Fatal("call ID filter did not return the selected call")
		}
		details := object(t, h.json(t, 0, append([]string{"calls", "show", id}, window...)...))
		if details["source"] != "private.call_details_data" {
			t.Error("private call details fell back to Data API")
		}
		mustRows(t, h.json(t, 0, append([]string{"calls", "legs", id}, window...)...))
		for _, flag := range []string{"--lost", "--transfer"} {
			mustRows(t, h.json(t, 0, append([]string{"calls", "search", "--source", "dataapi", "--limit", "1", flag}, window...)...))
		}
	})
	for _, kind := range []string{"topic", "contact"} {
		t.Run("live/support-"+kind, func(t *testing.T) { mustRows(t, h.json(t, 0, "support", kind, "list")) })
	}
	t.Run("live/support-write", func(t *testing.T) {
		if os.Getenv("UIS_E2E_SUPPORT_WRITE") != "1" {
			t.Skip("live support writes NOT tested; set UIS_E2E_SUPPORT_WRITE=1 with explicit fixture IDs and a new journal")
		}
		h.supportLifecycle(t, account)
	})
	t.Run("live/support-archive", func(t *testing.T) {
		live := object(t, h.json(t, 0, "support", "ticket", "list", "--live", "--limit", "1"))
		rows := mustRows(t, live["data"])
		if len(rows) == 0 {
			t.Fatal("account needs at least one support ticket as a read fixture")
		}
		id := mustID(t, rows[0])
		shown := object(t, h.json(t, 0, "support", "ticket", "show", id, "--live"))
		assertTicket(t, object(t, shown["data"]), id)
		sync := object(t, h.json(t, 0, "support", "sync"))
		if sync["ready"] != true || sync["degraded"] != false || sync["last_successful_sync"] == nil {
			t.Fatal("sync did not finish with a healthy archive")
		}
		// Disable both credentials and network routes for offline archive checks.
		t.Setenv("UIS_TOKEN", "")
		t.Setenv("UIS_HOST", "127.0.0.1:1")
		offline := a
		offline.CredentialProcess.Command = []string{filepath.Join(scratch, "must-not-run")}
		if err := profile.Save(localConfig, profile.Config{DefaultAccount: account, Accounts: map[string]profile.Account{account: offline}}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := profile.Save(localConfig, profile.Config{DefaultAccount: account, Accounts: map[string]profile.Account{account: a}}); err != nil {
				t.Error(err)
			}
		})
		for _, action := range []string{"status", "doctor"} {
			h.json(t, 0, "support", action)
		}
		list := object(t, h.json(t, 0, "support", "ticket", "list", "--limit", "10000"))
		found := false
		for _, row := range mustRows(t, list["data"]) {
			if portal.ID(row) == id {
				found = true
			}
		}
		if !found {
			t.Error("live ticket missing from local archive")
		}
		local := object(t, h.json(t, 0, "support", "ticket", "show", id))
		if local["source"] != "local" {
			t.Error("archive show used wrong source")
		}
		assertTicket(t, object(t, local["data"]), id)
		search := object(t, h.json(t, 0, "support", "ticket", "search", id, "--limit", "10000"))
		found = false
		for _, row := range mustRows(t, search["data"]) {
			if portal.ID(row) == id {
				found = true
			}
		}
		if !found {
			t.Error("FTS did not find the archived ticket by its indexed ID")
		}
	})
	t.Run("local/mutation-plans", func(t *testing.T) {
		for _, args := range [][]string{
			{"calls", "comment", "1", "--body", "E2E dry run"},
			{"calls", "tag", "1", "--add", "1"}, {"calls", "tag", "1", "--remove", "1"},
			{"support", "ticket", "create", "--topic-id", "1", "--contact-id", "1", "--phone-id", "1", "--email-id", "1", "--body", "E2E dry run"},
			{"support", "ticket", "reply", "1", "--body", "E2E dry run"},
			{"support", "ticket", "close", "1"}, {"support", "ticket", "reopen", "1"},
		} {
			h.json(t, 0, append([]string{"--dry-run"}, args...)...)
		}
		h.json(t, 2, "calls", "search", "--limit", "0")
		h.json(t, 10, "support", "ticket", "close", "1")
	})
	t.Run("local/all-method-plans", func(t *testing.T) {
		// Discover the public generated routes from help, then compare their RPC
		// plans to the independent embedded API specification.
		routes := map[string][]string{}
		resource := ""
		for _, line := range strings.Split(h.text(t, "--help"), "\n") {
			if line != "" && !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
				resource = strings.TrimSuffix(line, ":")
			}
			parts := strings.Fields(line)
			if len(parts) == 4 && parts[0] == "-" && parts[2] == "->" {
				routes[parts[3]] = []string{resource, parts[1]}
			}
		}
		methods, err := spec.Load()
		if err != nil || len(methods) == 0 {
			t.Fatal("embedded method spec missing")
		}
		for _, m := range methods {
			t.Run(m.Method, func(t *testing.T) {
				params := map[string]any{}
				for _, p := range m.Params {
					if !p.Required || p.Name == "access_token" {
						continue
					}
					var value any = "e2e"
					switch p.Type {
					case "number":
						value = 1
					case "boolean":
						value = true
					case "array":
						value = []any{}
					case "object":
						value = map[string]any{}
					case "iso8601":
						value = from
					}
					params[p.Name] = value
				}
				raw, _ := json.Marshal(params)
				route, ok := routes[m.Method]
				if !ok {
					t.Fatal("spec method missing from generated CLI help")
				}
				for _, command := range [][]string{{"call", m.Method}, route} {
					args := append([]string{"--dry-run"}, command...)
					args = append(args, "--params-json", string(raw))
					plan := object(t, h.json(t, 0, args...))
					if plan["method"] != m.Method {
						t.Error("dry-run routed to wrong RPC method")
					}
				}
			})
		}
	})
	t.Run("local/logout", func(t *testing.T) {
		h.json(t, 0, "--force", "auth", "logout", "--surface", "private")
		status := object(t, h.json(t, 0, "auth", "status"))
		if status["session_present"] != false {
			t.Error("logout retained the isolated session")
		}
	})
}

type harness struct{ binary string }

func (h harness) text(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.binary, args...)
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal("local CLI command failed (output suppressed)")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		t.Fatal("local CLI command produced no output")
	}
	return string(raw)
}

type cliResult struct {
	Schema  string `json:"schemaVersion"`
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Data    any    `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Details struct {
			TicketID   string `json:"ticket_id"`
			CampaignID string `json:"campaign_id"`
		} `json:"details"`
	} `json:"error"`
}

func (h harness) execute(t *testing.T, args ...string) (cliResult, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.binary, append([]string{"--json", "--no-input", "--timeout", "30s", "--retries", "0"}, args...)...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard // Never log provider bodies, PII or helper diagnostics.
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal("CLI execution failed (output suppressed)")
		}
		code = exit.ExitCode()
	}
	var envelope cliResult
	dec := json.NewDecoder(&stdout)
	dec.UseNumber()
	if dec.Decode(&envelope) != nil {
		t.Fatal("CLI did not emit a JSON envelope (output suppressed)")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || envelope.Schema != "uis.cli/v1" || envelope.Command == "" || envelope.OK != (code == 0) {
		t.Fatal("invalid CLI envelope contract (output suppressed)")
	}
	return envelope, code
}

func (h harness) json(t *testing.T, want int, args ...string) any {
	t.Helper()
	envelope, code := h.execute(t, args...)
	if code != want {
		t.Fatalf("CLI exit=%d, expected=%d, error=%s (output suppressed)", code, want, envelope.Error.Code)
	}
	if expected := map[int]string{2: "invalid_usage", 10: "confirmation_required"}[want]; expected != "" && envelope.Error.Code != expected {
		t.Fatal("error category does not match the documented exit code")
	}
	return envelope.Data
}
func object(t *testing.T, v any) map[string]any {
	t.Helper()
	o, ok := v.(map[string]any)
	if !ok {
		t.Fatal("expected JSON object")
	}
	return o
}
func mustRows(t *testing.T, v any) []portal.Object {
	t.Helper()
	rows, err := portal.Rows(v)
	if err != nil {
		t.Fatal("expected provider row collection")
	}
	return rows
}
func mustID(t *testing.T, v portal.Object) string {
	t.Helper()
	id := portal.ID(v)
	if id == "" {
		t.Fatal("fixture has no provider identity")
	}
	return id
}
func assertTicket(t *testing.T, v map[string]any, id string) {
	t.Helper()
	if mustID(t, object(t, v["ticket"])) != id {
		t.Error("ticket show returned a different identity")
	}
	if _, ok := v["comments"].([]any); !ok {
		t.Error("ticket comments missing")
	}
}
