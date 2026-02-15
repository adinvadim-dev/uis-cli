package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"uis-cli/internal/spec"
)

func TestGeneratedCoversAllSpecMethods(t *testing.T) {
	ms, err := spec.Load()
	if err != nil {
		t.Fatalf("spec.Load: %v", err)
	}
	if len(ms) == 0 {
		t.Fatalf("spec is empty")
	}
	if len(generatedResources) == 0 {
		t.Fatalf("generatedResources is empty (run: make gen)")
	}

	genByMethod := map[string]GenMethod{}
	for _, r := range generatedResources {
		for _, m := range r.Methods {
			if m.Method == "" {
				t.Fatalf("empty method in generated resource %q", r.Command)
			}
			if _, ok := genByMethod[m.Method]; ok {
				t.Fatalf("duplicate generated method: %s", m.Method)
			}
			genByMethod[m.Method] = m
		}
	}

	for _, sm := range ms {
		gm, ok := genByMethod[sm.Method]
		if !ok {
			t.Fatalf("spec method missing from generated CLI: %s", sm.Method)
		}
		if len(gm.Params) != len(sm.Params) {
			t.Fatalf("%s: params count mismatch: spec=%d gen=%d", sm.Method, len(sm.Params), len(gm.Params))
		}
		for i := range sm.Params {
			sp := sm.Params[i]
			gp := gm.Params[i]
			if gp.Name != sp.Name {
				t.Fatalf("%s: param[%d] name mismatch: spec=%q gen=%q", sm.Method, i, sp.Name, gp.Name)
			}
			if gp.Required != sp.Required {
				t.Fatalf("%s: param %s required mismatch: spec=%v gen=%v", sm.Method, sp.Name, sp.Required, gp.Required)
			}
			if gp.Flag == "" || gp.FlagJSON == "" {
				t.Fatalf("%s: param %s missing flag names: flag=%q json=%q", sm.Method, sp.Name, gp.Flag, gp.FlagJSON)
			}
			if gp.Flag == gp.FlagJSON {
				t.Fatalf("%s: param %s flag collision: %q", sm.Method, sp.Name, gp.Flag)
			}
		}
	}
}

func TestGeneratedCommandsHaveFlagsForAllParams(t *testing.T) {
	// Avoid reading the user's real config in tests.
	tmp := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(tmp, "config.json"))

	root := newRootCmd()

	for _, r := range generatedResources {
		for _, m := range r.Methods {
			cmd := findCmd(root, r.Command, m.Command)
			if cmd == nil {
				t.Fatalf("command not found: uis %s %s", r.Command, m.Command)
			}
			for _, p := range m.Params {
				if p.Name == "access_token" {
					continue
				}
				if cmd.Flags().Lookup(p.Flag) == nil {
					t.Fatalf("%s: missing flag --%s for param %s", m.Method, p.Flag, p.Name)
				}
				if cmd.Flags().Lookup(p.FlagJSON) == nil {
					t.Fatalf("%s: missing flag --%s for param %s", m.Method, p.FlagJSON, p.Name)
				}
			}
		}
	}
}

func TestDryRunWorksForAllCommandsWithRequiredParams(t *testing.T) {
	// Silence output: this test may execute many commands.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	oldOut := os.Stdout
	oldErr := os.Stderr
	os.Stdout = devNull
	os.Stderr = devNull
	defer func() {
		os.Stdout = oldOut
		os.Stderr = oldErr
	}()

	// Avoid reading the user's real config in tests.
	tmp := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(tmp, "config.json"))

	// Execute each command with --dry-run and placeholder required params.
	for _, r := range generatedResources {
		for _, m := range r.Methods {
			root := newRootCmd()
			args := []string{"--dry-run", r.Command, m.Command}
			for _, p := range m.Params {
				if !p.Required || p.Name == "access_token" {
					continue
				}
				// Prefer JSON flag for non-string types.
				switch p.Type {
				case "number":
					args = append(args, "--"+p.FlagJSON, "1")
				case "boolean":
					args = append(args, "--"+p.FlagJSON, "true")
				case "array":
					args = append(args, "--"+p.FlagJSON, "[]")
				case "object":
					args = append(args, "--"+p.FlagJSON, "{}")
				case "iso8601":
					// docs examples use "YYYY-MM-DD hh:mm:ss"
					if p.Name == "date_till" || p.Name == "date_to" {
						args = append(args, "--"+p.Flag, "2026-01-02 00:00:00")
					} else {
						args = append(args, "--"+p.Flag, "2026-01-01 00:00:00")
					}
				default:
					args = append(args, "--"+p.Flag, "x")
				}
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("dry-run failed for uis %s %s (%s): %v", r.Command, m.Command, m.Method, err)
			}
		}
	}
}

func TestDryRunJSONHasTypedCommonParams(t *testing.T) {
	// Avoid reading the user's real config in tests.
	tmp := t.TempDir()
	t.Setenv("UIS_CONFIG", filepath.Join(tmp, "config.json"))
	t.Setenv("UIS_TOKEN", "")

	type outBody struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}

	for _, r := range generatedResources {
		for _, m := range r.Methods {
			root := newRootCmd()

			args := []string{"--json", "--dry-run", r.Command, m.Command}
			expect := map[string]string{} // paramName -> kind

			// Required params.
			for _, p := range m.Params {
				if !p.Required || p.Name == "access_token" {
					continue
				}
				switch p.Type {
				case "number":
					args = append(args, "--"+p.Flag, "1")
					expect[p.Name] = "number"
				case "boolean":
					args = append(args, "--"+p.Flag, "true")
					expect[p.Name] = "boolean"
				case "array":
					args = append(args, "--"+p.FlagJSON, "[]")
					expect[p.Name] = "array"
				case "object":
					args = append(args, "--"+p.FlagJSON, "{}")
					expect[p.Name] = "object"
				case "iso8601":
					if p.Name == "date_till" || p.Name == "date_to" {
						args = append(args, "--"+p.Flag, "2026-01-02 00:00:00")
					} else {
						args = append(args, "--"+p.Flag, "2026-01-01 00:00:00")
					}
				default:
					args = append(args, "--"+p.Flag, "x")
				}
			}

			// Common optional params: ensure plain flags become typed JSON.
			for _, p := range m.Params {
				if p.Name == "access_token" || p.Required {
					continue
				}
				if p.Name == "limit" || p.Name == "offset" {
					args = append(args, "--"+p.Flag, "10")
					expect[p.Name] = "number"
				}
				if p.Name == "fields" && p.Type == "array" {
					args = append(args, "--"+p.Flag, "id,name")
					expect[p.Name] = "array"
				}
			}

			// Capture stdout (dry-run + --json outputs pure JSON).
			var buf bytes.Buffer
			old := os.Stdout
			rp, wp, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			os.Stdout = wp

			root.SetArgs(args)
			execErr := root.Execute()

			_ = wp.Close()
			_, _ = buf.ReadFrom(rp)
			_ = rp.Close()
			os.Stdout = old

			if execErr != nil {
				t.Fatalf("dry-run json failed for uis %s %s (%s): %v", r.Command, m.Command, m.Method, execErr)
			}

			var ob outBody
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &ob); err != nil {
				t.Fatalf("%s: unmarshal dry-run json: %v\nout=%s", m.Method, err, buf.String())
			}
			if ob.Method != m.Method {
				t.Fatalf("expected method %s, got %s", m.Method, ob.Method)
			}
			if ob.Params == nil {
				t.Fatalf("%s: missing params in dry-run output", m.Method)
			}

			for name, kind := range expect {
				v, ok := ob.Params[name]
				if !ok {
					t.Fatalf("%s: expected param %q in params map", m.Method, name)
				}
				switch kind {
				case "number":
					if _, ok := v.(float64); !ok {
						t.Fatalf("%s: param %s expected number, got %T (%v)", m.Method, name, v, v)
					}
				case "boolean":
					if _, ok := v.(bool); !ok {
						t.Fatalf("%s: param %s expected bool, got %T (%v)", m.Method, name, v, v)
					}
				case "array":
					if _, ok := v.([]any); !ok {
						t.Fatalf("%s: param %s expected array, got %T (%v)", m.Method, name, v, v)
					}
				case "object":
					if _, ok := v.(map[string]any); !ok {
						t.Fatalf("%s: param %s expected object, got %T (%v)", m.Method, name, v, v)
					}
				}
			}
		}
	}
}

func findCmd(root *cobra.Command, resource string, action string) *cobra.Command {
	rc, _, err := root.Find([]string{resource})
	if err != nil || rc == nil {
		return nil
	}
	ac, _, err := root.Find([]string{resource, action})
	if err != nil || ac == nil {
		return nil
	}
	return ac
}
