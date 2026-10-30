package portal

import (
	"context"
	"encoding/json"
	"fmt"
)

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
	id := ""
	if write {
		key := "campaign_id"
		if method == "set.auto_call_campaign_status" {
			key = "id"
		}
		id = fmt.Sprint(p[key])
		if !validCampaignID(id) {
			return nil, &Error{Code: 2, Stage: "autodial_rpc_campaign_id"}
		}
		if method == "set.auto_call_campaign_status" {
			if e := ValidateCampaignStatus(Object{"status": p["status"]}); e != nil {
				return nil, e
			}
		} else {
			b, e := json.Marshal(p["phone_numbers"])
			var nums []string
			if e != nil || json.Unmarshal(b, &nums) != nil {
				return nil, &Error{Code: 2, Stage: "autodial_rpc_phone_numbers"}
			}
			for _, n := range nums {
				if n == "" {
					return nil, &Error{Code: 2, Stage: "autodial_rpc_phone_numbers"}
				}
			}
		}
	}
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
		if write {
			if pe, ok := e.(*Error); ok {
				pe.CampaignID = id
			}
		}
		return nil, e
	}
	code := 7
	if write {
		code = 9
	}
	if v["id"] != "uis-autodial" {
		return nil, &Error{Code: code, CampaignID: id, Stage: "autodial_rpc_identity"}
	}
	result, ok := v["result"].(map[string]interface{})
	if !ok {
		return nil, &Error{Code: code, CampaignID: id, Stage: "autodial_rpc_result"}
	}
	if write {
		acknowledged := result["success"] == true
		if data, ok := result["data"].(map[string]interface{}); ok && len(data) == 0 {
			_, metadataOK := result["metadata"].(map[string]interface{})
			acknowledged = acknowledged || metadataOK
		}
		if !acknowledged {
			return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_acknowledgement"}
		}
		idKey := "campaign_id"
		if method == "set.auto_call_campaign_status" {
			idKey = "id"
		}
		id = fmt.Sprint(p[idKey])
		if !validCampaignID(id) {
			return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_readback_identity"}
		}
		after, e := c.AutodialRPC(ctx, "get.auto_call_campaigns", Object{"limit": 1, "filter": Object{"field": "id", "operator": "=", "value": json.Number(id)}, "fields": []string{"id", "status", "phone_numbers"}})
		if e != nil {
			return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_readback"}
		}
		rows := after["data"].([]interface{})
		if len(rows) != 1 {
			return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_readback"}
		}
		row, ok := rows[0].(map[string]interface{})
		if !ok || ID(row) != id {
			return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_readback_identity"}
		}
		if method == "set.auto_call_campaign_status" {
			if row["status"] != p["status"] {
				return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_status_not_observed"}
			}
		} else {
			phones, ok := row["phone_numbers"].([]interface{})
			if !ok {
				return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_phone_schema"}
			}
			states := map[string]string{}
			for _, v := range phones {
				o, ok := v.(map[string]interface{})
				if !ok {
					return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_phone_schema"}
				}
				num, ok := o["phone_number"].(string)
				state, ok2 := o["status"].(string)
				if !ok || !ok2 {
					return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_phone_schema"}
				}
				states[num] = state
			}
			requested := []string{}
			switch nums := p["phone_numbers"].(type) {
			case []string:
				requested = nums
			case []interface{}:
				for _, n := range nums {
					s, ok := n.(string)
					if !ok {
						return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_phone_schema"}
					}
					requested = append(requested, s)
				}
			default:
				return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_phone_schema"}
			}
			for _, n := range requested {
				state, present := states[n]
				if method == "create.auto_call_campaign_phone_numbers" && (!present || state == "deleted") {
					return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_add_not_observed"}
				}
				if method == "delete.auto_call_campaign_phone_numbers" && present && state != "deleted" {
					return nil, &Error{Code: 9, CampaignID: id, Stage: "autodial_rpc_delete_not_observed"}
				}
			}
		}
		result["success"] = true
		result["verified"] = true
	} else {
		if _, ok := result["data"].([]interface{}); !ok {
			return nil, &Error{Code: 7, Stage: "autodial_rpc_rows"}
		}
	}
	return result, nil
}
