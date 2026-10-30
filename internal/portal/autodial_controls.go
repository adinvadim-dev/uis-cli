package portal

import (
	"context"
	"encoding/json"
	"fmt"
)

func ValidateCampaignStatus(payload Object) error {
	if len(payload) != 1 {
		return &Error{Code: 2, Stage: "campaign_status_payload"}
	}
	switch payload["status"] {
	case "draft", "planned", "in_process", "paused", "completed", "cancelled":
		return nil
	}
	return &Error{Code: 2, Stage: "campaign_status_value"}
}

func (c *Client) changeAutodialCampaign(ctx context.Context, action, id string, payload Object) (interface{}, error) {
	if action == "status" {
		if e := ValidateCampaignStatus(payload); e != nil {
			return nil, e
		}
	}
	v, e := c.Autodial(ctx, "get", id, nil)
	if e != nil {
		return nil, e
	}
	current := v.(map[string]interface{})
	if action == "status" {
		target := payload["status"]
		if current["status"] == target {
			return Object{"success": true, "verified": true, "no_op": true, "campaign_id": json.Number(id)}, nil
		}
		v, e = c.Autodial(ctx, "statuses", "", nil)
		if e != nil {
			return nil, e
		}
		allowed := false
		for _, r := range v.([]interface{}) {
			o, ok := r.(map[string]interface{})
			if !ok {
				return nil, &Error{Code: 7, Stage: "campaign_status_schema"}
			}
			if o["id"] == current["status"] {
				next, ok := o["next_statuses"].([]interface{})
				if !ok {
					return nil, &Error{Code: 7, Stage: "campaign_status_schema"}
				}
				for _, n := range next {
					if n == target {
						allowed = true
					}
				}
			}
		}
		if !allowed {
			return nil, &Error{Code: 7, Stage: "campaign_transition_unavailable", CampaignID: id}
		}
		return c.AutodialRPC(ctx, "set.auto_call_campaign_status", Object{"id": json.Number(id), "status": target})
	}
	if len(payload) != 0 {
		return nil, &Error{Code: 2, Stage: "campaign_clear_payload"}
	}
	phones, ok := campaignPhones(current["phone_list"])
	if !ok {
		return nil, &Error{Code: 7, Stage: "campaign_phone_schema"}
	}
	remove := []string{}
	for _, p := range phones {
		if p["status_mnemonic"] == "deleted" {
			continue
		}
		if p["status_mnemonic"] == "successfully_processed" || p["status_mnemonic"] == "processed_outside_campaign" {
			return nil, &Error{Code: 7, Stage: "campaign_final_contact_cannot_be_removed", CampaignID: id}
		}
		remove = append(remove, fmt.Sprint(p["numa"]))
	}
	if len(remove) == 0 {
		return Object{"success": true, "verified": true, "no_op": true, "campaign_id": json.Number(id), "active_contacts": 0}, nil
	}
	return c.AutodialRPC(ctx, "delete.auto_call_campaign_phone_numbers", Object{"campaign_id": json.Number(id), "phone_numbers": remove})
}
