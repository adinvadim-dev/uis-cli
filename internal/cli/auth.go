package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"uis-cli/internal/config"
)

func newAuthCmd(g *globalFlags) *cobra.Command {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Authentication helpers (stores token in config)",
	}

	var tokenStdin bool
	var tokenFile string

	login := &cobra.Command{
		Use:   "login",
		Short: "Store UIS access token in config",
		Long: strings.TrimSpace(`
Store UIS access token in the uis-cli config file.

Prefer:
  - UIS_TOKEN env var (for CI/secrets managers), or
  - this command, which stores it in a local config file with 0600 perms.

Secrets are not accepted via flags.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}

			var tok string
			switch {
			case tokenStdin:
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return exitf(2, "Failed reading token from stdin: %v", err)
				}
				tok = strings.TrimSpace(string(b))
				if tok == "" {
					return exitf(2, "Empty token on stdin")
				}
			case tokenFile != "":
				b, err := os.ReadFile(tokenFile)
				if err != nil {
					return exitf(2, "Failed reading --token-file %q: %v", tokenFile, err)
				}
				tok = strings.TrimSpace(string(b))
				if tok == "" {
					return exitf(2, "Empty token in file %q", tokenFile)
				}
			default:
				tok, err = promptToken(g.NoInput)
				if err != nil {
					return err
				}
			}

			cfg.AccessToken = tok
			if err := config.Save(cfgPath, cfg); err != nil {
				return exitf(1, "Failed writing config %s: %v", cfgPath, err)
			}
			if !g.Quiet {
				where := cfgDirHint(cfgPath)
				_, _ = fmt.Fprintf(os.Stderr, "Token saved to %s\n", where)
			}
			return nil
		},
	}
	login.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read token from stdin (one line or full stdin)")
	login.Flags().StringVar(&tokenFile, "token-file", "", "Read token from a file (use a secrets manager file mount)")

	logout := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored token from config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}
			if cfg.AccessToken == "" {
				if !g.Quiet {
					_, _ = fmt.Fprintln(os.Stderr, "No token stored.")
				}
				return nil
			}
			cfg.AccessToken = ""
			if err := config.Save(cfgPath, cfg); err != nil {
				return exitf(1, "Failed writing config %s: %v", cfgPath, err)
			}
			if !g.Quiet {
				_, _ = fmt.Fprintln(os.Stderr, "Token removed.")
			}
			return nil
		},
	}

	whoami := &cobra.Command{
		Use:   "status",
		Short: "Show where the token is coming from (env vs config) without printing it",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}
			source := "config"
			if os.Getenv("UIS_TOKEN") != "" {
				source = "env"
			}
			has := cfg.AccessToken != ""
			out := "missing"
			if has {
				out = "present"
			}
			_, _ = fmt.Fprintf(os.Stdout, "token=%s source=%s config=%s\n", out, source, cfgPath)
			return nil
		},
	}

	auth.AddCommand(login, logout, whoami)
	return auth
}
