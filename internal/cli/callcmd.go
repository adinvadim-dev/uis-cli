package cli

import (
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

			params, err := parseParams(paramKVs, paramsJSON, paramsFile)
			if err != nil {
				return err
			}
			if handled, e := runAutodialRPC(g, cmd, method, params); handled {
				return e
			}
			// UIS APIs typically require access_token in params.
			if cfg.AccessToken != "" {
				if _, ok := params["access_token"]; !ok {
					params["access_token"] = cfg.AccessToken
				}
			}

			return runRPC(g, method, params)
		},
	}

	call.Flags().String("source", "auto", "Outgoing campaign authentication: auto, private or dataapi")
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
