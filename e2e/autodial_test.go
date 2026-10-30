//go:build production

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"uis-cli/internal/profile"
)

// Read-only supervisor probe, independently selectable from the support suite.
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
	for _, action := range []string{"statuses", "employees", "stop-list"} {
		t.Run(action, func(t *testing.T) {
			v := object(t, h.json(t, 0, "autodial", action))
			if v["source"] != "private.supervisor" || v["data"] == nil {
				t.Fatal("missing source/data")
			}
		})
	}
}
