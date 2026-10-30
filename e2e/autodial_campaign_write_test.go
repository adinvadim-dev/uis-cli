//go:build production

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This intentionally leaves ONE cancelled campaign with logically deleted
// synthetic rows. Never opt in when a previous journal remains unreconciled.
func (h harness) autodialCampaignLifecycle(t *testing.T, account string, payloadFile func(string, any) string) {
	t.Helper()
	if os.Getenv("UIS_E2E_AUTODIAL_CAMPAIGN_WRITE") != "1" {
		t.Skip("fresh campaign write opt-in required; no create proof from this test")
	}
	if os.Getenv("UIS_E2E_AUTODIAL_ALLOW_CANCELLED_RESIDUE") != "1" {
		t.Fatal("explicit permission for one cancelled residue required")
	}
	caller, employee := os.Getenv("UIS_E2E_AUTODIAL_CALLER_ID"), os.Getenv("UIS_E2E_AUTODIAL_EMPLOYEE_ID")
	if !providerID.MatchString(caller) || !providerID.MatchString(employee) {
		t.Fatal("explicit caller and employee fixtures required")
	}
	path := os.Getenv("UIS_E2E_AUTODIAL_CAMPAIGN_JOURNAL")
	if !filepath.IsAbs(path) {
		t.Fatal("new absolute journal required")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("journal already exists or cannot be claimed; reconcile, never replay creation")
	}
	defer f.Close()
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		t.Fatal("cannot generate marker")
	}
	name := "uis-cli e2e test — do not start " + hex.EncodeToString(nonce[:])
	id := ""
	stage := func(s string) {
		t.Helper()
		if json.NewEncoder(f).Encode(map[string]any{"account": account, "name": name, "campaign_id": id, "stage": s}) != nil || f.Sync() != nil {
			t.Fatal("cannot persist lifecycle stage; stop and reconcile")
		}
	}
	read := func() map[string]any {
		t.Helper()
		c := object(t, object(t, h.json(t, 0, "autodial", "get", id))["data"])
		if c["name"] != name {
			t.Fatal("test campaign identity mismatch")
		}
		return c
	}
	stage("prepared")
	// Account-local 2099 start, disabled audio, and synthetic numbers only. A
	// create becomes planned even when callers would have preferred a draft.
	p := map[string]any{"name": name, "begin_date": "2099-01-01 10:00:00", "employee_ids": []json.Number{json.Number(employee)}, "phone_list": []any{map[string]any{"numa": "70000000000"}}, "number_capacity_usage_rule": "fixed", "number_capacity_id": json.Number(caller), "is_subscriber_message_enabled": false, "subscriber_time_from": "10:00", "subscriber_time_till": "20:00", "attempts": map[string]any{"subscriber_busy": []string{"00:15:00"}, "subscriber_not_responsible": []string{"00:15:00"}, "subscriber_number_problems": []string{"00:15:00"}, "other": []string{"00:15:00"}}}
	stage("create_dispatch")
	r, code := h.execute(t, "--force", "--timeout", "120s", "autodial", "create", "--payload-file", payloadFile("campaign-create", p))

	if code == 0 {
		receipt := object(t, object(t, r.Data)["data"])
		id = providerIDString(receipt["campaign_id"])
		if receipt["verified"] != true {
			code = 9
		}
	} else {
		id = r.Error.Details.CampaignID
	}
	if !providerID.MatchString(id) {
		stage("create_unresolved")
		t.Fatal("create did not return identity; reconcile marker before any further create")
	}
	// An outcome_unknown with a known ID still permits cleanup after proving
	// ownership, but it never turns the failed create into a passing test.
	stage("create_returned") // Persist the known identity before fallible ownership readback.
	read()
	stage("identity_verified")
	createCode := code
	pendingWrite := false

	// Defer cancellation only after ownership is proven. On every failure the
	// journal records where cleanup stopped; there is no automatic write retry.
	defer func() {
		if pendingWrite {
			stage("campaign_write_unresolved")
			t.Error("unresolved campaign mutation: retain journal and reconcile before cleanup")
			return
		}
		c := read()
		if c["status"] != "planned" && c["status"] != "cancelled" {
			stage("unsafe_status_observed")
			t.Error("unexpected campaign status; manual reconciliation required")
			return
		}
		stage("clear_dispatch")
		r, code := h.execute(t, "--force", "autodial", "clear", id)
		if code != 0 {
			stage("clear_unresolved")
			t.Error("contact cleanup unresolved")
			return
		}
		if object(t, object(t, r.Data)["data"])["verified"] != true {
			stage("clear_unresolved")
			t.Error("clear not verified")
			return
		}
		stage("cancel_dispatch")
		r, code = h.execute(t, "--force", "autodial", "cancel", id)
		if code != 0 {
			stage("cancel_unresolved")
			t.Error("cancel unresolved")
			return
		}
		c = read()
		if c["status"] != "cancelled" {
			stage("cancel_unresolved")
			t.Error("cancel not observed")
			return
		}
		for _, v := range c["phone_list"].([]any) {
			if object(t, v)["status_mnemonic"] != "deleted" {
				stage("cleanup_unresolved")
				t.Error("active synthetic row remains")
				return
			}
		}
		stage("cleanup_verified")
	}()
	if createCode != 0 {
		t.Fatal("create returned outcome_unknown; owned campaign cleanup will run")
	}
	c := read()
	if c["status"] != "planned" {
		t.Fatal("future campaign must be planned")
	}
	rows := object(t, h.json(t, 0, "autodial", "list", "--payload-file", payloadFile("campaign-list", map[string]any{"begin_date": "2099-01-01 00:00:00", "end_date": "2099-01-02 00:00:00", "search_text": name, "statuses": []string{}})))["data"].([]any)
	found := false
	for _, v := range rows {
		if providerIDString(object(t, v)["id"]) == id {
			found = true
		}
	}
	if !found {
		t.Fatal("created campaign absent from list")
	}
	pendingWrite = true
	stage("update_dispatch")
	h.json(t, 0, "--force", "autodial", "update", id, "--payload-file", payloadFile("campaign-update", map[string]any{"employee_ids": []json.Number{json.Number(employee)}, "phone_list": []any{map[string]any{"numa": "70000000000"}, map[string]any{"numa": "70000000001"}}}))
	pendingWrite = false
	stage("update_verified")
	// Planned settings are read-only; pretending success would hide a lost edit.
	r, code = h.execute(t, "--force", "autodial", "update", id, "--payload-file", payloadFile("campaign-readonly", map[string]any{"begin_date": "2099-01-02 10:00:00"}))
	if code != 7 || r.Error.Code != "capability_unavailable" {
		t.Fatal("planned date edit must fail closed")
	}
	for _, action := range []string{"create", "delete"} {
		pendingWrite = true
		stage("phone_" + action + "_dispatch")
		h.json(t, 0, "--force", "auto-call-campaign-phone-numbers", action, "--source", "private", "--campaign-id", id, "--phone-numbers-json", `["70000000002"]`)
		pendingWrite = false
		stage("phone_" + action + "_verified")
	}
	for _, action := range []string{"calls", "subscribers"} {
		h.json(t, 0, "autodial", action, id)
	}
	h.json(t, 0, "autodial", "export", "--payload-file", payloadFile("campaign-export", map[string]any{"campaign_ids": []json.Number{json.Number(id)}, "numa_statuses": []string{"not_processed", "unsuccessfully_processed"}}))
}

func providerIDString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case json.Number:
		return n.String()
	}
	return ""
}
