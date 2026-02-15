package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCallCmd(g *globalFlags) *cobra.Command {
	var paramsJSON string
	var paramsFile string
	var paramKVs []string

	call := &cobra.Command{
		Use:   "call <method>",
		Short: "Call any UIS JSON-RPC method by name (low-level)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := args[0]

			_, cfg, err := loadEffectiveConfig(g)
			if err != nil {
				return err
			}
			if cfg.AccessToken == "" && !g.DryRun {
				return exitf(10, "Missing access token. Set UIS_TOKEN env var or run `uis auth login`.")
			}

			params, err := parseParams(paramKVs, paramsJSON, paramsFile)
			if err != nil {
				return err
			}
			// UIS APIs typically require access_token in params.
			if cfg.AccessToken != "" {
				if _, ok := params["access_token"]; !ok {
					params["access_token"] = cfg.AccessToken
				}
			}

			if g.DryRun {
				return writeDryRunRequest(g, method, params)
			}

			c, err := newClient(cfg)
			if err != nil {
				return err
			}

			ctx, cancel, err := ctxWithTimeout(cfg)
			if err != nil {
				return err
			}
			defer cancel()

			resp, httpStatus, raw, err := c.Call(ctx, method, params)
			if err != nil {
				if httpStatus == 401 || httpStatus == 403 {
					return exitf(10, "HTTP %d calling %s: %v", httpStatus, method, err)
				}
				if httpStatus == 429 {
					return exitf(11, "HTTP 429 calling %s: %v", method, err)
				}
				return exitf(12, "HTTP error calling %s: %v", method, err)
			}

			if resp.Error != nil {
				msg := resp.Error.String()
				code := 12
				if strings.Contains(strings.ToLower(msg), "token") || strings.Contains(strings.ToLower(msg), "auth") {
					code = 10
				}
				return exitf(code, "%s: %s", method, msg)
			}

			if len(resp.Result) > 0 {
				return writeResult(g, resp.Result)
			}

			// Some APIs may return no result; show raw.
			_, _ = fmt.Fprintln(os.Stdout, string(raw))
			return nil
		},
	}

	call.Flags().StringArrayVar(&paramKVs, "param", nil, "Method param: key=value or key:=<json> (repeatable)")
	call.Flags().StringVar(&paramsJSON, "params-json", "", "Params as a JSON object string")
	call.Flags().StringVar(&paramsFile, "params-file", "", "Read params JSON object from file ('-' for stdin)")

	// Example-rich help.
	call.Example = strings.TrimSpace(`
	  # call a method with string params
	  uis call get.employees --param date_from=2026-01-01 --param date_to=2026-01-31

	  # pass non-string values via := (JSON)
	  uis call get.calls --param limit:=100 --param offset:=0

	  # pass a full params object
	  uis call get.employees --params-json '{"date_from":"2026-01-01","date_to":"2026-01-31"}'

	  # output JSON (result only)
	  uis --json call get.campaigns --param limit:=10
	`)

	return call
}
