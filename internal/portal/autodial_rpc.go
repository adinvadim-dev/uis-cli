package portal

import "context"

// IsAutodialRPC bounds the keyless cabinet route to the outgoing-campaign
// methods. Advertising campaigns and arbitrary public RPCs are not implied.
func IsAutodialRPC(method string) bool {
	switch method {
	case "get.auto_call_campaigns", "create.auto_call_campaign_phone_numbers", "delete.auto_call_campaign_phone_numbers", "set.auto_call_campaign_status":
		return true
	}
	return false
}

// AutodialRPC uses the cabinet JWT on the RPC host, not an API key parameter.
// A write is sent once after the configured binding/authentication preflight.
// Read access does not establish entitlement to mutations; provider rejection
// remains an error and an ambiguous response must never cause a blind replay.
func (c *Client) AutodialRPC(ctx context.Context, method string, params Object) (Object, error) {
	if !IsAutodialRPC(method) {
		return nil, &Error{Code: 7, Stage: "autodial_rpc_unavailable"}
	}
	p := Object{}
	for k, v := range params {
		if k != "access_token" {
			p[k] = v
		}
	}
	write := method != "get.auto_call_campaigns"
	if c.Session.CabinetToken == "" {
		return nil, &Error{Code: 3, Stage: "cabinet_session_refresh_required"}
	}
	if write {
		if e := c.preflight(ctx); e != nil {
			return nil, e
		}
	}
	v, e := c.cabinetRequest(ctx, true, Object{"jsonrpc": "2.0", "id": "uis-autodial", "method": method, "params": p}, write)
	if e != nil {
		return nil, e
	}
	code := 7
	if write {
		code = 9
	}
	if v["id"] != "uis-autodial" {
		return nil, &Error{Code: code, Stage: "autodial_rpc_identity"}
	}
	result, ok := v["result"].(map[string]interface{})
	if !ok {
		return nil, &Error{Code: code, Stage: "autodial_rpc_result"}
	}
	if write {
		if result["success"] != true {
			return nil, &Error{Code: 9, Stage: "autodial_rpc_acknowledgement"}
		}
	} else {
		if _, ok := result["data"].([]interface{}); !ok {
			return nil, &Error{Code: 7, Stage: "autodial_rpc_rows"}
		}
	}
	return result, nil
}
