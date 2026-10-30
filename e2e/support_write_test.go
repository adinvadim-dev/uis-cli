//go:build production

package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"uis-cli/internal/portal"
)

var providerID = regexp.MustCompile(`^[1-9][0-9]*$`)

type writeJournal struct {
	Account  string `json:"account"`
	Marker   string `json:"marker"`
	TicketID string `json:"ticket_id,omitempty"`
	Stage    string `json:"stage"`
}

func (h harness) supportLifecycle(t *testing.T, account string) {
	t.Helper()
	// Reject incomplete setup before claiming the journal or sending any writes.
	args := []string{"--force", "support", "ticket", "create"}
	for _, fixture := range []struct{ flag, env string }{
		{"--topic-id", "UIS_E2E_TOPIC_ID"}, {"--contact-id", "UIS_E2E_CONTACT_ID"},
		{"--phone-id", "UIS_E2E_PHONE_ID"}, {"--email-id", "UIS_E2E_EMAIL_ID"},
	} {
		id := os.Getenv(fixture.env)
		if !providerID.MatchString(id) {
			t.Fatalf("%s must be an explicit positive provider ID", fixture.env)
		}
		args = append(args, fixture.flag, id)
	}
	path := os.Getenv("UIS_E2E_WRITE_JOURNAL")
	if !filepath.IsAbs(path) {
		t.Fatal("UIS_E2E_WRITE_JOURNAL must be an absolute new durable file path")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("write journal locked; reconcile the previous process before removing its lock")
	}
	lock.Close()
	defer os.Remove(path + ".lock")
	resume := os.Getenv("UIS_E2E_SUPPORT_RESUME") == "1"
	j := writeJournal{Account: account, Stage: "prepared"}
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY | os.O_APPEND
	if resume {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("cannot read existing recovery journal")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		for {
			var record writeJournal
			if err := decoder.Decode(&record); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal("journal has an incomplete record; reconcile manually")
			}
			j = record
		}
		if j.Account != account || !providerID.MatchString(j.TicketID) || !regexp.MustCompile(`^uis-cli-e2e-[0-9a-f]{32}$`).MatchString(j.Marker) || (j.Stage != "create_returned" && j.Stage != "reopened_verified_dispatch") {
			t.Fatal("resume requires a known created ticket before reply, or read-only reconciliation of a dispatched reopen")
		}
		flags = os.O_WRONLY | os.O_APPEND
	} else {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			t.Fatal("cannot generate test marker")
		}
		j.Marker = "uis-cli-e2e-" + hex.EncodeToString(random[:])
	}
	// Exclusive creation is intentional: reruns must not create duplicate tickets,
	// even after an interrupted or outcome_unknown create. Never erase this file.
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		t.Fatal("cannot claim write journal; use an existing private parent directory and a new file (never overwrite a previous run)")
	}
	defer f.Close()
	save := func(stage string) {
		t.Helper()
		j.Stage = stage
		if err := persistJournal(f, j); err != nil {
			t.Fatal("cannot persist write journal; stop and reconcile the ticket manually")
		}
	}
	if !resume {
		save("prepared")
	}
	body := "Автоматическая E2E-проверка CLI UIS. Тестовое обращение, помощь не требуется, инцидента нет. Проверим ответ, закрытие и переоткрытие, затем закроем. Маркер: " + j.Marker
	reply := "Тестовый ответ E2E, помощь не требуется. Маркер: " + j.Marker
	// Capture a known ticket ID even when create's initial-comment stage fails.
	if !resume {
		save("create_dispatch")
		result, code := h.execute(t, append(args, "--body", body)...)
		if o, ok := result.Data.(map[string]any); ok {
			j.TicketID = portal.ID(o)
		}
		if j.TicketID == "" {
			j.TicketID = result.Error.Details.TicketID
		}
		save("create_returned")
		if code != 0 {
			t.Fatalf("support create exit=%d, error=%s; journal retained, do not resend", code, result.Error.Code)
		}
	}
	if !providerID.MatchString(j.TicketID) {
		t.Fatal("create returned no valid ticket ID; reconcile using the journal marker, do not resend")
	}
	t.Logf("test ticket id=%s; journal records marker and progress", j.TicketID)
	read := func() map[string]any {
		t.Helper()
		shown := object(t, h.json(t, 0, "support", "ticket", "show", j.TicketID, "--live"))
		data := object(t, shown["data"])
		assertTicket(t, data, j.TicketID)
		ticket := object(t, data["ticket"])
		if ticket["context"] != body {
			t.Fatal("ticket marker/body mismatch; refusing further mutations")
		}
		return data
	}
	expectMessage := func(data map[string]any, message string) {
		t.Helper()
		count := 0
		for _, comment := range mustRows(t, data["comments"]) {
			if comment["message"] == message {
				count++
			}
		}
		if count != 1 {
			t.Fatal("expected exactly one persisted test message; journal retained, do not resend")
		}
	}
	expectStatus := func(data map[string]any, closed bool) {
		t.Helper()
		got := object(t, data["ticket"])["status"]
		isClosed := got == "close_customer" || got == "close_operator" || got == "canceled"
		if (closed && !isClosed) || (!closed && got != "created" && got != "waiting_operator") {
			t.Fatal("live ticket status does not match the requested transition")
		}
	}
	data := read()
	expectStatus(data, false)
	expectMessage(data, body)
	if resume && j.Stage == "reopened_verified_dispatch" {
		// The previous command may have succeeded despite an uncertain ack.
		// Prove its effect with this fresh read; never send reopen again.
		expectMessage(data, reply)
		save("reopened_verified")
		t.Log("reconciled the previously dispatched reopen by reading; only final close will be sent")
	} else {
		save("created_verified")
		// Every dispatch occurs once, and only after the preceding read has verified
		// this run's ticket. Failures leave progress intact for manual reconciliation.
		save("reply_dispatch")
		comment := object(t, h.json(t, 0, "--force", "support", "ticket", "reply", j.TicketID, "--body", reply))
		commentID := mustID(t, comment)
		data = read()
		expectMessage(data, body)
		expectMessage(data, reply)
		found := false
		for _, row := range mustRows(t, data["comments"]) {
			if portal.ID(row) == commentID && row["message"] == reply {
				found = true
			}
		}
		if !found {
			t.Fatal("reply identity not present in the live thread")
		}
		save("reply_verified")
	}
	for _, step := range []struct {
		action, stage string
		closed        bool
	}{
		{"close", "closed_verified", true}, {"reopen", "reopened_verified", false}, {"close", "complete", true},
	} {
		if resume && j.Stage == "reopened_verified" && step.stage != "complete" {
			continue
		}
		save(step.stage + "_dispatch")
		changed := object(t, h.json(t, 0, "--force", "support", "ticket", step.action, j.TicketID))
		if mustID(t, changed) != j.TicketID {
			t.Fatal("mutation response returned a different ticket")
		}
		data = read()
		expectStatus(data, step.closed)
		expectMessage(data, body)
		expectMessage(data, reply)
		save(step.stage)
	}
}

func persistJournal(f *os.File, j writeJournal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	// Append-only snapshots retain the marker and last known ticket identity
	// even if a later snapshot is interrupted or cannot be written.
	_, err = f.Write(append(raw, '\n'))
	if err == nil {
		err = f.Sync()
	}
	return err
}

func TestJournalRetainsRecoveryHistoryAfterTornAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, record := range []writeJournal{
		{Account: "team", Marker: "test-marker", Stage: "create_dispatch"},
		{Account: "team", Marker: "test-marker", TicketID: "123", Stage: "create_returned"},
	} {
		if err := persistJournal(f, record); err != nil {
			t.Fatal(err)
		}
	}
	// Model interruption midway through the next snapshot. Completed recovery
	// records must remain usable even though the final JSON document is torn.
	if _, err := f.Write([]byte(`{"stage":`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var record writeJournal
	if err := decoder.Decode(&record); err != nil || record.Stage != "create_dispatch" || record.Marker != "test-marker" {
		t.Fatal("original recovery marker was lost")
	}
	if err := decoder.Decode(&record); err != nil || record.Stage != "create_returned" || record.TicketID != "123" {
		t.Fatal("last complete ticket identity was lost")
	}
	if err := decoder.Decode(&record); err != io.ErrUnexpectedEOF {
		t.Fatal("expected the interrupted tail to remain distinguishable from completed records")
	}
}
