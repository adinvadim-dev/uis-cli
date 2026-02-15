package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"uis-cli/internal/spec"
)

func newMethodsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "methods",
		Short: "Inspect embedded UIS method metadata (from docs spec)",
	}

	var verb string
	var q string
	list := &cobra.Command{
		Use:   "list",
		Short: "List method names",
		RunE: func(cmd *cobra.Command, args []string) error {
			ms, err := spec.Load()
			if err != nil {
				return exitf(1, "Failed to load embedded spec: %v", err)
			}
			needle := strings.ToLower(strings.TrimSpace(q))
			for _, m := range ms {
				if verb != "" && !strings.HasPrefix(m.Method, verb+".") {
					continue
				}
				if needle != "" && !strings.Contains(strings.ToLower(m.Method), needle) {
					continue
				}
				_, _ = fmt.Fprintln(os.Stdout, m.Method)
			}
			return nil
		},
	}
	list.Flags().StringVar(&verb, "verb", "", "Filter by verb (get/create/update/delete/...)")
	list.Flags().StringVar(&q, "q", "", "Substring query (case-insensitive)")

	show := &cobra.Command{
		Use:   "show <method>",
		Short: "Show one method's params and docs URL (if present)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ms, err := spec.Load()
			if err != nil {
				return exitf(1, "Failed to load embedded spec: %v", err)
			}
			for _, m := range ms {
				if m.Method != name {
					continue
				}
				// Render as plain text for humans (AI-friendly too).
				_, _ = fmt.Fprintf(os.Stdout, "Method: %s\n", m.Method)
				if m.Title != "" {
					_, _ = fmt.Fprintf(os.Stdout, "Title:  %s\n", m.Title)
				}
				if m.URL != "" {
					_, _ = fmt.Fprintf(os.Stdout, "Docs:   %s\n", m.URL)
				}
				if m.Description != "" {
					_, _ = fmt.Fprintf(os.Stdout, "\n%s\n", m.Description)
				}
				if len(m.Params) > 0 {
					_, _ = fmt.Fprintln(os.Stdout, "\nParams:")
					for _, p := range m.Params {
						req := "optional"
						if p.Required {
							req = "required"
						}
						line := fmt.Sprintf("  - %s (%s)", p.Name, req)
						if p.Type != "" {
							line += " type=" + p.Type
						}
						if p.Description != "" {
							line += " : " + p.Description
						}
						_, _ = fmt.Fprintln(os.Stdout, line)
					}
				}
				return nil
			}
			return exitf(2, "Unknown method %q (try: uis methods list --q %s)", name, name)
		},
	}

	cmd.AddCommand(list, show)
	return cmd
}
