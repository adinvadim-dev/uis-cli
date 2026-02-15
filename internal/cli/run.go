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

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"uis-cli/internal/client"
	"uis-cli/internal/config"
)

type globalFlags struct {
	ConfigPath string

	Host       string
	APIVersion string
	Timeout    string
	Retries    int
	RetriesSet bool

	JSON bool
	DryRun bool

	Pretty  bool
	Quiet   bool
	Verbose bool
	NoInput bool
}

func Run(args []string) int {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		var ee *ExitError
		if errors.As(err, &ee) {
			_, _ = fmt.Fprintln(os.Stderr, ee.Msg)
			return ee.Code
		}
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:           "uis",
		Short:         "CLI for UIS Data API (JSON-RPC)",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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
	root.PersistentFlags().BoolVar(&g.DryRun, "dry-run", false, "Do not call the API; print the JSON-RPC request that would be sent")
	root.PersistentFlags().BoolVar(&g.Pretty, "pretty", false, "Pretty-print JSON output (default: true on TTY)")
	root.PersistentFlags().BoolVarP(&g.Quiet, "quiet", "q", false, "Suppress non-essential output (errors still go to stderr)")
	root.PersistentFlags().BoolVarP(&g.Verbose, "verbose", "v", false, "Verbose diagnostics to stderr")
	root.PersistentFlags().BoolVar(&g.NoInput, "no-input", false, "Disable prompts; fail if required input is missing")

	root.AddCommand(newAuthCmd(g))
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
	if err != nil {
		return "", config.Config{}, exitf(1, "Failed to load config %s: %v", cfgPath, err)
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
	if err != nil {
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
