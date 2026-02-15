package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"uis-cli/internal/config"
)

func newConfigCmd(g *globalFlags) *cobra.Command {
	cfgCmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and modify uis-cli config",
	}

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the resolved config file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout, cfgPath)
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print effective config (after env/flags overrides)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}
			return writeJSON(g, cfg)
		},
	}

	var host, apiVersion, timeout string
	var retries int
	var setToken bool
	var tokenStdin bool

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Persist config values (host/api-version/timeout/retries). Use `uis auth login` for token.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}

			if host != "" {
				cfg.APIHost = host
			}
			if apiVersion != "" {
				cfg.APIVersion = apiVersion
			}
			if timeout != "" {
				cfg.Timeout = timeout
			}
			if retries != 0 {
				cfg.Retries = retries
			}

			// Escape hatch for automation: allow token set via stdin only.
			if setToken {
				if !tokenStdin {
					return exitf(2, "Refusing to set token via flags. Use --set-token --token-stdin and pipe the token on stdin, or use `uis auth login`.")
				}
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return exitf(2, "Failed reading token from stdin: %v", err)
				}
				cfg.AccessToken = strings.TrimSpace(string(b))
			}

			if err := config.Save(cfgPath, cfg); err != nil {
				return exitf(1, "Failed writing config %s: %v", cfgPath, err)
			}
			if !g.Quiet {
				_, _ = fmt.Fprintf(os.Stderr, "Config saved: %s\n", cfgPath)
			}
			return nil
		},
	}
	setCmd.Flags().StringVar(&host, "host", "", "Set api_host")
	setCmd.Flags().StringVar(&apiVersion, "api-version", "", "Set api_version")
	setCmd.Flags().StringVar(&timeout, "timeout", "", "Set timeout duration string, e.g. 60s")
	setCmd.Flags().IntVar(&retries, "retries", 0, "Set retries")
	setCmd.Flags().BoolVar(&setToken, "set-token", false, "Set token (requires --token-stdin); intended for automation only")
	setCmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read token from stdin (with --set-token)")

	envCmd := &cobra.Command{
		Use:   "env",
		Short: "Print current env overrides relevant to uis-cli",
		RunE: func(cmd *cobra.Command, args []string) error {
			m := map[string]any{
				"UIS_CONFIG":      os.Getenv("UIS_CONFIG"),
				"UIS_TOKEN":       redacted(os.Getenv("UIS_TOKEN")),
				"UIS_HOST":        os.Getenv("UIS_HOST"),
				"UIS_API_VERSION": os.Getenv("UIS_API_VERSION"),
				"UIS_TIMEOUT":     os.Getenv("UIS_TIMEOUT"),
				"UIS_RETRIES":     parseIntMaybe(os.Getenv("UIS_RETRIES")),
			}
			return writeJSON(g, m)
		},
	}

	cfgCmd.AddCommand(pathCmd, showCmd, setCmd, envCmd)
	return cfgCmd
}

func redacted(v string) string {
	if v == "" {
		return ""
	}
	return "<redacted>"
}

func parseIntMaybe(s string) any {
	if s == "" {
		return ""
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return s
	}
	return n
}
