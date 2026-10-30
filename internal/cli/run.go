package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"uis-cli/internal/client"
	"uis-cli/internal/config"
	"uis-cli/internal/profile"
)

type globalFlags struct {
	ConfigPath string

	Host       string
	APIVersion string
	Timeout    string
	Retries    int
	RetriesSet bool

	Account string
	Command string
	Plain   bool
	Force   bool
	JSON    bool
	DryRun  bool

	Pretty  bool
	Quiet   bool
	Verbose bool
	NoInput bool
}

func Run(args []string) int {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	if executed, err := cmd.ExecuteC(); err != nil {
		code := 1
		var ee *ExitError
		if errors.As(err, &ee) {
			code = ee.Code
		}
		jsonOutput, _ := cmd.PersistentFlags().GetBool("json")
		if jsonOutput {
			names := map[int]string{1: "execution_failed", 2: "invalid_usage", 3: "authentication_required", 4: "permission_denied", 5: "not_found", 6: "provider_error", 7: "capability_unavailable", 8: "rate_limited", 9: "outcome_unknown", 10: "confirmation_required"}
			details := map[string]interface{}{}
			if ee != nil && ee.Details != nil {
				details = ee.Details
			}
			b, _ := json.Marshal(map[string]interface{}{"ok": false, "schemaVersion": "uis.cli/v1", "command": strings.TrimPrefix(executed.CommandPath(), "uis "), "error": map[string]interface{}{"code": names[code], "message": safeError(code), "details": details}})
			fmt.Fprintln(os.Stdout, string(b))
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		return code
	}

	return 0
}

func newRootCmd() *cobra.Command {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:           "uis",
		Short:         "UIS support, calls and Data API",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			g.Command = strings.TrimPrefix(cmd.CommandPath(), "uis ")
			if g.JSON && g.Plain {
				return exitf(2, "--json and --plain are mutually exclusive")
			}
			if _, err := timeout(g); err != nil {
				return err
			}
			// Track which flags were explicitly set (so config/env can still win otherwise).
			if cmd.Root().PersistentFlags().Changed("retries") {
				g.RetriesSet = true
			}

			// Default pretty output when attached to a TTY.
			if cmd.Root().PersistentFlags().Changed("pretty") == false {
				g.Pretty = isatty.IsTerminal(os.Stdout.Fd())
			}
			return nil
		},
	}

	root.PersistentFlags().StringVar(&g.ConfigPath, "config", "", "Config file path (default: user config dir)")
	root.PersistentFlags().StringVar(&g.Host, "host", "", "API host (env: UIS_HOST)")
	root.PersistentFlags().StringVar(&g.APIVersion, "api-version", "", "API version path, e.g. v2.0 (env: UIS_API_VERSION)")
	root.PersistentFlags().StringVar(&g.Timeout, "timeout", "", "HTTP timeout (duration), e.g. 60s (env: UIS_TIMEOUT)")
	root.PersistentFlags().IntVar(&g.Retries, "retries", 3, "Retry count for transient HTTP errors (env: UIS_RETRIES)")

	root.PersistentFlags().BoolVar(&g.JSON, "json", false, "Output result as JSON (default: human-readable)")
	root.PersistentFlags().BoolVarP(&g.DryRun, "dry-run", "n", false, "Do not call the API; print the JSON-RPC request that would be sent")
	root.PersistentFlags().BoolVar(&g.Pretty, "pretty", false, "Pretty-print JSON output (default: true on TTY)")
	root.PersistentFlags().BoolVarP(&g.Quiet, "quiet", "q", false, "Suppress non-essential output (errors still go to stderr)")
	root.PersistentFlags().BoolVarP(&g.Verbose, "verbose", "v", false, "Verbose diagnostics to stderr")
	root.PersistentFlags().BoolVar(&g.NoInput, "no-input", false, "Disable prompts; fail if required input is missing")

	root.PersistentFlags().StringVar(&g.Account, "account", "", "Account profile (UIS_ACCOUNT)")
	root.PersistentFlags().BoolVar(&g.Plain, "plain", false, "Stable plain output")
	root.PersistentFlags().BoolVarP(&g.Force, "force", "f", false, "Confirm requested mutation")
	root.PersistentFlags().Bool("no-color", false, "Disable color")
	root.AddCommand(newAuthCmd(g))
	root.AddCommand(newAccounts(g), newCalls(g), newSupport(g), newAutodial(g))
	root.AddCommand(newDiscovery(g, "filters", map[string]string{"list": "get.report_filters", "show": "get.custom_filters", "rapid": "get.report_rapid_filters"}))
	root.AddCommand(newDiscovery(g, "reports", map[string]string{"list": "get.reports_list"}))
	capability := &cobra.Command{Use: "capability"}
	for _, action := range []string{"list", "probe"} {
		capability.AddCommand(&cobra.Command{Use: action, RunE: func(cmd *cobra.Command, args []string) error {
			return emit(g, map[string]interface{}{"dataapi": "available_with_credentials", "support_archive": "local-sqlite", "private_reads": "requires_promoted_session", "private_login": "credential_binding_and_authenticated_read", "private_mutations": "promoted_session_plus_authenticated_read_probe", "rapid_saved_filters": "provider_entitlement_required"})
		}})
	}
	root.AddCommand(capability)
	root.AddCommand(newConfigCmd(g))
	root.AddCommand(newSpecCmd(g))
	root.AddCommand(newCallCmd(g))
	root.AddCommand(newMethodsCmd(g))
	addGeneratedResourceCommands(root, g)

	root.Version = version()
	root.SetVersionTemplate("{{.Version}}\n")

	// Human-first help: show all documented UIS methods grouped by verb on `uis --help`.
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		if cmd == root {
			_ = appendResourcesToHelp(cmd)
		}
	})

	return root
}

func version() string {
	// Keep it simple for now; can wire in -ldflags later.
	return "0.1.0"
}

func loadEffectiveConfig(g *globalFlags) (string, config.Config, error) {
	cfgPath := g.ConfigPath
	if cfgPath == "" {
		if env := os.Getenv("UIS_CONFIG"); env != "" {
			cfgPath = env
		} else {
			p, err := config.DefaultPath()
			if err != nil {
				return "", config.Config{}, err
			}
			cfgPath = p
		}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil && strings.HasSuffix(cfgPath, ".toml") {
		_, profileErr := profile.Load(cfgPath)
		cfg = config.Default()
		err = profileErr
	}
	if err != nil {
		return "", config.Config{}, exitf(1, "Failed to load config %s: %v", cfgPath, err)
	}

	profiles := profile.Config{}
	var profileErr error
	if !strings.HasSuffix(profileConfigPath(g), ".json") {
		profiles, profileErr = profile.Load(profileConfigPath(g))
	}
	if profileErr != nil {
		return "", config.Config{}, exitf(2, "Invalid profile config")
	}
	if g.Account != "" || os.Getenv("UIS_ACCOUNT") != "" || profiles.DefaultAccount != "" {
		cfg.AccessToken = ""
	}
	// Env overrides
	if v := os.Getenv("UIS_TOKEN"); v != "" {
		cfg.AccessToken = v
	}
	if v := os.Getenv("UIS_HOST"); v != "" {
		cfg.APIHost = v
	}
	if v := os.Getenv("UIS_API_VERSION"); v != "" {
		cfg.APIVersion = v
	}
	if v := os.Getenv("UIS_TIMEOUT"); v != "" {
		cfg.Timeout = v
	}
	if v := os.Getenv("UIS_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Retries = n
		}
	}

	// Flag overrides
	if g.Host != "" {
		cfg.APIHost = g.Host
	}
	if g.APIVersion != "" {
		cfg.APIVersion = g.APIVersion
	}
	if g.Timeout != "" {
		cfg.Timeout = g.Timeout
	}
	if g.RetriesSet {
		cfg.Retries = g.Retries
	}

	return cfgPath, cfg, nil
}

func newClient(cfg config.Config) (*client.Client, error) {
	to, err := cfg.TimeoutDuration()
	if err != nil || to <= 0 || to > 5*time.Minute {
		return nil, exitf(2, "Invalid timeout %q (expected duration like 60s): %v", cfg.Timeout, err)
	}
	base := fmt.Sprintf("https://%s/%s", strings.TrimSuffix(cfg.APIHost, "/"), strings.TrimPrefix(cfg.APIVersion, "/"))
	return &client.Client{BaseURL: base, Timeout: to, Retries: cfg.Retries}, nil
}

func parseParams(paramKVs []string, paramsJSON string, paramsFile string) (map[string]any, error) {
	out := map[string]any{}

	if paramsJSON != "" {
		var v any
		if err := json.Unmarshal([]byte(paramsJSON), &v); err != nil {
			return nil, exitf(2, "Invalid --params-json: %v", err)
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, exitf(2, "--params-json must be a JSON object")
		}
		for k, vv := range obj {
			out[k] = vv
		}
	}

	if paramsFile != "" {
		var b []byte
		var err error
		if paramsFile == "-" {
			b, err = io.ReadAll(os.Stdin)
		} else {
			b, err = os.ReadFile(paramsFile)
		}
		if err != nil {
			return nil, exitf(2, "Failed reading --params-file %q: %v", paramsFile, err)
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, exitf(2, "Invalid JSON in --params-file %q: %v", paramsFile, err)
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, exitf(2, "--params-file JSON must be an object")
		}
		for k, vv := range obj {
			out[k] = vv
		}
	}

	for _, raw := range paramKVs {
		// key=value -> string
		// key:=<json> -> parsed JSON
		if strings.Contains(raw, ":=") {
			parts := strings.SplitN(raw, ":=", 2)
			k := strings.TrimSpace(parts[0])
			if k == "" {
				return nil, exitf(2, "Invalid --param %q: empty key", raw)
			}
			var v any
			if err := json.Unmarshal([]byte(parts[1]), &v); err != nil {
				return nil, exitf(2, "Invalid JSON in --param %s: %v", k, err)
			}
			out[k] = v
			continue
		}
		if strings.Contains(raw, "=") {
			parts := strings.SplitN(raw, "=", 2)
			k := strings.TrimSpace(parts[0])
			if k == "" {
				return nil, exitf(2, "Invalid --param %q: empty key", raw)
			}
			out[k] = parts[1]
			continue
		}
		return nil, exitf(2, "Invalid --param %q: expected key=value or key:=<json>", raw)
	}

	return out, nil
}

func writeJSON(g *globalFlags, v any) error {
	if g.JSON {
		v = map[string]interface{}{"ok": true, "schemaVersion": "uis.cli/v1", "command": g.Command, "data": v}
	}
	var b []byte
	var err error
	if g.Pretty {
		b, err = json.MarshalIndent(v, "", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = os.Stdout.Write(b)
	return err
}

func promptToken(noInput bool) (string, error) {
	if noInput {
		return "", exitf(2, "Missing token. Provide it via UIS_TOKEN env or use `uis auth login` (interactive) / `uis auth login --token-stdin`")
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return "", exitf(2, "stdin is not a TTY. Use `uis auth login --token-stdin` to read token from stdin")
	}

	_, _ = fmt.Fprint(os.Stderr, "UIS access token: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", exitf(2, "Empty token")
	}
	return tok, nil
}

func cfgDirHint(path string) string {
	dir := filepath.Dir(path)
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(dir, home) {
		return strings.Replace(dir, home, "~", 1)
	}
	return dir
}

func ctxWithTimeout(cfg config.Config) (context.Context, context.CancelFunc, error) {
	to, err := cfg.TimeoutDuration()
	if err != nil {
		return nil, nil, exitf(2, "Invalid timeout %q: %v", cfg.Timeout, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	return ctx, cancel, nil
}

func safeError(code int) string {
	return map[int]string{1: "Execution failed", 2: "Invalid command or parameters", 3: "Credentials or private session required", 4: "Permission denied", 5: "Not found", 6: "Provider request failed", 7: "Capability unavailable; see capability list and docs/IMPLEMENTATION.md", 8: "Rate limited", 9: "Outcome unknown; do not resend automatically", 10: "Confirmation required; review --dry-run then use --force"}[code]
}
