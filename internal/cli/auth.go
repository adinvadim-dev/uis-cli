package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"uis-cli/internal/config"
	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func newAuthCmd(g *globalFlags) *cobra.Command {
	r := &cobra.Command{Use: "auth", Short: "External credentials and private session lifecycle"}
	for _, action := range []string{"login", "refresh", "status", "logout"} {
		action := action
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			name, a, e := profileFor(g)
			if e != nil {
				return e
			}
			surface, _ := cmd.Flags().GetString("surface")
			if surface != "dataapi" && surface != "private" {
				return exitf(2, "surface must be dataapi or private")
			}
			if g.DryRun {
				return emit(g, map[string]interface{}{"account": name, "action": action, "surface": surface})
			}
			switch action {
			case "status":
				s, e := portal.LoadSession(profile.State(name))
				return emit(g, map[string]interface{}{"account": name, "session_present": e == nil, "principal": s.Login, "updated_at": s.UpdatedAt, "credential_process_configured": len(a.CredentialProcess.Command) > 0, "dataapi_env_present": os.Getenv("UIS_TOKEN") != "", "private_login": "credential_binding_and_authenticated_read"})
			case "logout":
				if e = confirm(g, map[string]interface{}{"logout": name}); e != nil {
					return e
				}
				if surface == "dataapi" {
					path, cfg, err := loadEffectiveConfig(g)
					if err != nil {
						return err
					}
					removed := false
					if !strings.HasSuffix(path, ".toml") && cfg.AccessToken != "" && os.Getenv("UIS_TOKEN") == "" {
						disk, err := config.Load(path)
						if err != nil {
							return err
						}
						if disk.AccessToken != "" {
							disk.AccessToken = ""
							removed = true
							if err = config.Save(path, disk); err != nil {
								return err
							}
						}
					}
					return emit(g, map[string]interface{}{"account": name, "surface": surface, "local_token_removed": removed, "external_credentials_unchanged": true})
				}
				e = os.Remove(filepath.Join(profile.State(name), "session.json"))
				if e != nil && !os.IsNotExist(e) {
					return e
				}
				return emit(g, map[string]interface{}{"account": name, "logged_out": true})
			default:
				if surface == "private" {
					if portal.NormalizeLogin(a.ExpectedLogin) == "" {
						return exitf(3, "Configure expected_login before private login")
					}
					if a.ExpectedCustomerID != "" {
						return exitf(7, "Customer identity probe unavailable; previous session preserved")
					}
					to, err := timeout(g)
					if err != nil {
						return err
					}
					ctx := cmd.Context()
					fields, err := profile.Resolve(ctx, a, "portal.login", "portal.password")
					if err != nil {
						return exitf(3, "%s", err)
					}
					err = portal.LoginAndPromote(ctx, profile.State(name), a.ExpectedLogin, a.ExpectedCustomerID, fields["portal.login"], fields["portal.password"], to, func() (string, error) {
						f, e := profile.Resolve(ctx, a, "captcha.2captcha_api_key")
						if e != nil {
							return "", exitf(3, "%s", e)
						}
						return f["captcha.2captcha_api_key"], nil
					})
					if err != nil {
						var ce *portal.CaptchaError
						if errors.As(err, &ce) {
							return exitf(6, "%s", ce)
						}
						return operationError(err)
					}
					return emit(g, map[string]interface{}{"account": name, "surface": "private", "promoted": true, "binding": "credential_login_and_authenticated_read"})
				}
				_, effective, err := loadEffectiveConfig(g)
				if err != nil {
					return err
				}
				if effective.AccessToken != "" {
					return emit(g, map[string]interface{}{"account": name, "credential_present": true, "verified": false, "surface": "dataapi"})
				}
				_, e := profile.Resolve(context.Background(), a, "dataapi.access_token")
				if e != nil {
					return exitf(3, "%s", e)
				}
				return emit(g, map[string]interface{}{"account": name, "credential_present": true, "verified": false, "surface": "dataapi"})
			}
		}}
		c.Flags().String("surface", "dataapi", "dataapi or private")
		r.AddCommand(c)
	}
	return r
}
