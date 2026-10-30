package cli

import (
	"fmt"
	"github.com/spf13/cobra"
	"uis-cli/internal/archive"
	"uis-cli/internal/portal"
)

// runAutodialRPC handles only the four outgoing methods; unrelated generated
// methods retain their existing transport. Explicit dataapi retains the key path.
func runAutodialRPC(g *globalFlags, cmd *cobra.Command, method string, params map[string]interface{}) (bool, error) {
	source, _ := cmd.Flags().GetString("source")
	if source != "auto" && source != "private" && source != "dataapi" {
		return true, exitf(2, "source must be auto, private or dataapi")
	}
	if !portal.IsAutodialRPC(method) {
		if source == "private" {
			return true, exitf(7, "Private RPC routing is available only for outgoing campaign methods")
		}
		return false, nil
	}
	if source == "dataapi" {
		return false, nil
	}
	if g.DryRun && source == "auto" {
		return false, nil
	}
	// Validate required identities/status before any session I/O, including raw
	// call routes. Provider-specific eligibility and transitions remain server-owned.
	switch method {
	case "create.auto_call_campaign_phone_numbers", "delete.auto_call_campaign_phone_numbers":
		if _, e := positive(fmt.Sprint(params["campaign_id"])); e != nil {
			return true, e
		}
		switch v := params["phone_numbers"].(type) {
		case []interface{}:
			for _, x := range v {
				if _, ok := x.(string); !ok {
					return true, exitf(2, "phone_numbers must contain strings")
				}
			}
		case []string:
		default:
			return true, exitf(2, "phone_numbers must be an array")
		}
	case "set.auto_call_campaign_status":
		if _, e := positive(fmt.Sprint(params["id"])); e != nil {
			return true, e
		}
		switch params["status"] {
		case "draft", "planned", "in_process", "paused", "completed", "cancelled":
		default:
			return true, exitf(2, "Invalid outgoing campaign status")
		}
	}
	p := map[string]interface{}{}
	for k, v := range params {
		if k != "access_token" {
			p[k] = v
		}
	}
	if g.DryRun {
		// Auto is unresolved without reading state; preserve the legacy RPC plan.
		if source == "auto" {
			return false, nil
		}
		return true, emit(g, portal.Object{"source": "private.cabinet_rpc", "method": method, "params": redactParams(p), "network": false})
	}
	write := method != "get.auto_call_campaigns"
	if write {
		if e := confirm(g, portal.Object{"method": method, "source": source, "params": redactParams(p)}); e != nil {
			return true, e
		}
	}
	c, e := selectCallClient(g, source)
	if e != nil {
		return true, e
	}
	if c == nil {
		if write {
			confirmed := *g
			confirmed.Force = true
			return true, runRPC(&confirmed, method, params)
		}
		return false, nil
	}
	v, e := c.AutodialRPC(cmd.Context(), method, p)
	if e != nil {
		return true, operationError(e)
	}
	// Keep result data/metadata at the same level as the key-based route, adding
	// provenance so callers can use either source without confusing list shapes.
	out := archive.Scrub(v).(map[string]interface{})
	out["source"] = "private.cabinet_rpc"
	return true, emit(g, out)
}
