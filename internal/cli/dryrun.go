package cli

import (
	"fmt"
	"os"
	"strings"
)

func writeDryRunRequest(g *globalFlags, method string, params map[string]any) error {
	// Keep this stable: it's used for validation and for scripting previews.
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	if g.JSON {
		return writeJSON(g, body)
	}

	host := strings.TrimSpace(g.Host)
	if host == "" {
		host = "<default>"
	}
	apiVersion := strings.TrimSpace(g.APIVersion)
	if apiVersion == "" {
		apiVersion = "<default>"
	}

	_, _ = fmt.Fprintf(os.Stdout, "DRY RUN (no network)\nmethod: %s\nhost: %s\napi-version: %s\n\nbody:\n", method, host, apiVersion)
	return writeJSON(g, body)
}
