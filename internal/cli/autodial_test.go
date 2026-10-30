package cli

import (
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
	for _, action := range []string{"create", "update", "status", "exclude", "include", "listen"} {
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
