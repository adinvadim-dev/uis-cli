package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"uis-cli/internal/spec"
)

func newSpecCmd(g *globalFlags) *cobra.Command {
	specCmd := &cobra.Command{
		Use:   "spec",
		Short: "Work with the embedded UIS API method spec (generated from docs)",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List known API methods from the embedded spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			ms, err := spec.Load()
			if err != nil {
				return exitf(1, "Failed to load embedded spec: %v", err)
			}
			for _, m := range ms {
				_, _ = fmt.Fprintln(os.Stdout, m.Method)
			}
			return nil
		},
	}

	var method string
	show := &cobra.Command{
		Use:   "show",
		Short: "Show one method's spec entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			ms, err := spec.Load()
			if err != nil {
				return exitf(1, "Failed to load embedded spec: %v", err)
			}
			for _, m := range ms {
				if m.Method == method {
					return writeJSON(g, m)
				}
			}
			return exitf(2, "Unknown method %q (try: uis spec list)", method)
		},
	}
	show.Flags().StringVar(&method, "method", "", "Method name like get.employees")
	_ = show.MarkFlagRequired("method")

	var prefix string
	search := &cobra.Command{
		Use:   "search",
		Short: "Search methods by substring match",
		RunE: func(cmd *cobra.Command, args []string) error {
			ms, err := spec.Load()
			if err != nil {
				return exitf(1, "Failed to load embedded spec: %v", err)
			}
			needle := strings.ToLower(prefix)
			out := make([]spec.Method, 0)
			for _, m := range ms {
				if needle == "" || strings.Contains(strings.ToLower(m.Method), needle) {
					out = append(out, m)
				}
			}
			for _, m := range out {
				_, _ = fmt.Fprintln(os.Stdout, m.Method)
			}
			return nil
		},
	}
	search.Flags().StringVar(&prefix, "q", "", "Query substring (case-insensitive)")

	specCmd.AddCommand(list, show, search)
	return specCmd
}
