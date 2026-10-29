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
		"params":  redactParams(params),
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

func redactParams(params map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range params {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "cookie") || strings.Contains(lower, "csrf") {
			out[k] = "<redacted>"
			continue
		}
		switch x := v.(type) {
		case map[string]interface{}:
			out[k] = redactParams(x)
		case []interface{}:
			out[k] = redactArray(x)
		default:
			out[k] = v
		}
	}
	return out
}

func redactArray(xs []interface{}) []interface{} {
	out := make([]interface{}, len(xs))
	for i, v := range xs {
		switch x := v.(type) {
		case map[string]interface{}:
			out[i] = redactParams(x)
		case []interface{}:
			out[i] = redactArray(x)
		default:
			out[i] = v
		}
	}
	return out
}
