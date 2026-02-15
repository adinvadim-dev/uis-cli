package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"uis-cli/internal/spec"
)

func appendMethodsToHelp(cmd *cobra.Command) error {
	ms, err := spec.Load()
	if err != nil || len(ms) == 0 {
		return err
	}

	byVerb := map[string][]string{}
	for _, m := range ms {
		parts := spec.SplitMethod(m.Method)
		if len(parts) == 0 {
			continue
		}
		verb := parts[0]
		rest := strings.Join(parts[1:], ".")
		if rest == "" {
			rest = m.Method
		}
		byVerb[verb] = append(byVerb[verb], rest)
	}

	verbs := make([]string, 0, len(byVerb))
	for v := range byVerb {
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "\nDocumented UIS methods (embedded spec, %d total):\n", len(ms))
	_, _ = fmt.Fprintln(out, "  Tip: use resource commands (e.g. `uis campaigns list`) or `uis call <method>` for raw access.")

	for _, verb := range verbs {
		items := byVerb[verb]
		sort.Strings(items)
		_, _ = fmt.Fprintf(out, "\n%s (%d):\n", verb, len(items))
		for _, it := range items {
			_, _ = fmt.Fprintf(out, "  - %s.%s\n", verb, it)
		}
	}

	return nil
}
