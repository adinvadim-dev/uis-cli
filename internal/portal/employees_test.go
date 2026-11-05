package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func staffFixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c := New(Session{GoToken: "fixture-session"}, time.Second)
	c.BaseURL = s.URL
	c.Verify = func(context.Context, Session) error { return nil }
	return c
}

// Keep valid synthetic cards faithful to the observed cabinet schema while each
// scenario varies only the independent behavior it exercises.
func staffEmployeeFixture(t *testing.T, body string) string {
	t.Helper()
	var o Object
	d := json.NewDecoder(strings.NewReader(body))
	d.UseNumber()
	if err := d.Decode(&o); err != nil {
		t.Fatal(err)
	}
	if _, ok := o["full_name"]; !ok {
		o["full_name"] = "Synthetic Operator"
	}
	va, _ := o["va_settings"].(map[string]interface{})
	if va == nil {
		va = Object{}
		o["va_settings"] = va
	}
	for _, k := range []string{"is_need_hide_numbers", "is_click_to_call_auto_answer_enabled", "is_out_calls_auto_answer_enabled"} {
		if _, ok := va[k]; !ok {
			va[k] = false
		}
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertStaffCode(t *testing.T, err error, code int) {
	t.Helper()
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != code {
		t.Fatalf("expected code %d, got %v", code, err)
	}
}

// Public diagnostic output is an allowlisted projection. Unknown nested provider
// fields must not turn a successful read into a credential or network-locator dump.
func TestStaffReadsProjectDiagnostics(t *testing.T) {
	employee := staffEmployeeFixture(t, `{"id":11,"first_name":"Synthetic","last_name":"Operator","status_name":"Available","is_sip_registered":false,"staff_group_ids":[21],"staff_group_names":["Fixture group"],"password":"SECRET_PASSWORD","sip_uri":"sip:PRIVATE_URI@example.invalid","access_token":"SECRET_TOKEN","ip":"192.0.2.55","va_settings":{"is_active":true,"is_need_hide_numbers":true,"is_click_to_call_auto_answer_enabled":false,"is_out_calls_auto_answer_enabled":true,"password":"SECRET_PASSWORD"}}`)
	group := `{"id":21,"name":"Fixture group","manager_id":11,"password":"SECRET_PASSWORD"}`
	sip := `{"id":31,"employee_id":11,"employee_full_name":"Synthetic Operator","is_registered":false,"state":"active","channels_count":1,"is_multiterminal":true,"phone_number":"sip:PRIVATE_URI@example.invalid","password":"SECRET_PASSWORD","sip_line_registration":[{"start_time":"2026-11-05T00:00:00Z","is_closed":true,"user_agent":"Fixture phone","ip":"192.0.2.55","password":"SECRET_PASSWORD"}]}`
	c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("read dispatched %s", r.Method)
			w.WriteHeader(500)
			return
		}
		if len(r.URL.Query()) != 0 {
			t.Errorf("unexpected pagination/filter on observed complete-array surface: %s", r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/common/employee-staff":
			io.WriteString(w, "["+employee+"]")
		case "/common/employee-staff/11":
			io.WriteString(w, employee)
		case "/common/employee-group":
			io.WriteString(w, "["+group+"]")
		case "/common/employee-group/21":
			io.WriteString(w, group)
		case "/pbx/employee-sip-line":
			io.WriteString(w, "["+sip+`,{"id":32,"employee_id":12,"is_registered":true,"state":"active"}]`)
		case "/pbx/employee-sip-line/31":
			io.WriteString(w, sip)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	for _, tc := range []struct {
		name string
		run  func() (Object, error)
	}{
		{"employees list", func() (Object, error) { return c.StaffList(context.Background(), "employees", "") }},
		{"employee card", func() (Object, error) { return c.StaffGet(context.Background(), "employees", "11") }},
		{"groups list", func() (Object, error) { return c.StaffList(context.Background(), "groups", "") }},
		{"group card", func() (Object, error) { return c.StaffGet(context.Background(), "groups", "21") }},
		{"employee groups", func() (Object, error) { return c.StaffGroups(context.Background(), "11") }},
		{"softphone", func() (Object, error) { return c.StaffSoftphone(context.Background(), "11") }},
		{"sip list", func() (Object, error) { return c.StaffList(context.Background(), "sip-lines", "11") }},
		{"sip card", func() (Object, error) { return c.StaffGet(context.Background(), "sip-lines", "31") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			if out["source"] != "private.cabinet" || out["observed_at"] == "" {
				t.Fatal("missing read provenance/freshness")
			}
			b, _ := json.Marshal(out)
			for _, secret := range []string{"SECRET_PASSWORD", "PRIVATE_URI", "SECRET_TOKEN", "192.0.2.55"} {
				if strings.Contains(string(b), secret) {
					t.Errorf("read leaked %s", secret)
				}
			}
			switch tc.name {
			case "employee card":
				data := out["data"].(Object)
				if data["is_sip_registered"] != false || data["status_name"] != "Available" {
					t.Fatal("employee status and SIP registration must remain separate")
				}
			case "softphone":
				data := out["data"].(Object)
				settings := data["settings"].(Object)
				if settings["is_need_hide_numbers"] != true || settings["is_click_to_call_auto_answer_enabled"] != false || settings["is_out_calls_auto_answer_enabled"] != true {
					t.Fatal("lost softphone booleans")
				}
			case "employee groups":
				if len(out["data"].(Object)["groups"].([]Object)) != 1 {
					t.Fatal("membership join failed")
				}
			case "sip list":
				rows := out["data"].([]Object)
				if len(rows) != 1 || ID(rows[0]) != "31" {
					t.Fatal("employee SIP filter leaked another employee")
				}
			case "sip card":
				data := out["data"].(Object)
				if data["is_registered"] != false || data["state"] != "active" {
					t.Fatal("billing activity was mistaken for SIP registration")
				}
			}
		})
	}
}

func TestStaffProjectionRejectsNestedSecretSmuggling(t *testing.T) {
	for _, tc := range []struct{ resource, body string }{
		{"employees", `{"id":11,"staff_group_names":[{"password":"NESTED_PASSWORD","sip_uri":"sip:NESTED_URI@example.invalid","token":"NESTED_TOKEN","ip":"192.0.2.99"}]}`},
		{"groups", `{"id":11,"name":"Fixture group","employee_licenses":[{"password":"NESTED_PASSWORD","sip_uri":"sip:NESTED_URI@example.invalid","token":"NESTED_TOKEN","ip":"192.0.2.99"}]}`},
		{"sip-lines", `{"id":11,"is_registered":false,"state":"active","department_ids":[{"password":"NESTED_PASSWORD","sip_uri":"sip:NESTED_URI@example.invalid","token":"NESTED_TOKEN","ip":"192.0.2.99"}],"sip_line_registration":[{"user_agent":"Fixture 192.0.2.99"}]}`},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			if tc.resource == "employees" {
				tc.body = staffEmployeeFixture(t, tc.body)
			}
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.resource == "employees" && r.URL.Path == "/pbx/employee-sip-line" {
					io.WriteString(w, `[]`)
					return
				}
				io.WriteString(w, tc.body)
			})
			out, err := c.StaffGet(context.Background(), tc.resource, "11")
			if err != nil {
				assertStaffCode(t, err, 7)
				return
			} // A fail-closed drift error is also safe.
			b, _ := json.Marshal(out)
			for _, s := range []string{"NESTED_PASSWORD", "NESTED_URI", "NESTED_TOKEN", "192.0.2.99"} {
				if strings.Contains(string(b), s) {
					t.Errorf("unsafe nested provider value escaped projection: %s", s)
				}
			}
		})
	}
}

func TestStaffReadsFailOnIdentityDrift(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		list       bool
	}{
		{"wrong id", `{"id":12}`, false}, {"missing id", `{"first_name":"Fixture"}`, false},
		{"non object", `[]`, false}, {"duplicate ids", `[{"id":11},{"id":11}]`, true},
		{"missing list identity", `[{"first_name":"Fixture"}]`, true}, {"login html", `<html>login</html>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "duplicate ids" {
				card := staffEmployeeFixture(t, `{"id":11}`)
				tc.body = "[" + card + "," + card + "]"
			}
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) })
			var err error
			if tc.list {
				_, err = c.StaffList(context.Background(), "employees", "")
			} else {
				_, err = c.StaffGet(context.Background(), "employees", "11")
			}
			assertStaffCode(t, err, 7)
		})
	}
	for _, body := range []string{`{"id":11,"staff_group_ids":[21]}`, `{"id":11,"staff_group_ids":"21"}`} {
		body = staffEmployeeFixture(t, body)
		c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/common/employee-group" {
				io.WriteString(w, `[]`)
			} else {
				io.WriteString(w, body)
			}
		})
		_, err := c.StaffGroups(context.Background(), "11")
		assertStaffCode(t, err, 7)
	}
}

func TestStaffPatchValidationBeforeIO(t *testing.T) {
	tests := []struct{ name, resource, body string }{
		{"empty", "employees", `{}`}, {"password", "employees", `{"password":"SECRET"}`}, {"rights", "employees", `{"app_role":"admin"}`}, {"license", "employees", `{"va_settings":{"is_active":true}}`},
		{"empty surname", "employees", `{"last_name":" "}`}, {"wrong text type", "employees", `{"first_name":false}`}, {"bad reference", "employees", `{"schedule_id":0}`}, {"duplicate groups", "employees", `{"staff_group_ids":[21,21]}`},
		{"wrong softphone type", "softphone", `{"is_need_hide_numbers":"true"}`}, {"softphone password", "softphone", `{"password":"SECRET"}`}, {"enable license", "softphone", `{"is_active":true}`},
		{"reset disguised as state", "sip-lines", `{"state":"reset"}`}, {"limit_lock", "sip-lines", `{"state":"limit_lock"}`}, {"zero channels", "sip-lines", `{"channels_count":0}`}, {"excess channels", "sip-lines", `{"channels_count":3}`}, {"fractional channels", "sip-lines", `{"channels_count":1.5}`}, {"SIP password", "sip-lines", `{"password":"SECRET"}`}, {"SIP URI", "sip-lines", `{"uri":"sip:SECRET@example.invalid"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var patch Object
			d := json.NewDecoder(strings.NewReader(tc.body))
			d.UseNumber()
			if err := d.Decode(&patch); err != nil {
				t.Fatal(err)
			}
			calls := 0
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
			c.Verify = func(context.Context, Session) error { calls++; return nil }
			_, err := c.StaffUpdate(context.Background(), tc.resource, "11", patch)
			assertStaffCode(t, err, 2)
			if calls != 0 {
				t.Fatal("invalid patch performed preflight/network I/O")
			}
		})
	}
}

// Editor writes preserve observed writable phone/license/caller-ID blocks while
// omitting authentication, rights and computed summaries from the request.
func TestStaffUpdatePreservesDTOAndVerifies(t *testing.T) {
	for _, tc := range []struct {
		resource, body string
		patch          Object
	}{
		{"employees", `{"id":11,"last_name":"Old","login":"PRIVATE_LOGIN","password":"PRIVATE_PASSWORD","api_token":"PRIVATE_TOKEN","app_role_id":51,"is_token_active":true,"is_sip_registered":true,"va_settings":{"is_active":true,"phones":[{"id":90,"phone":"sip:untouched@example.invalid","priority":1}]},"omni_settings":{"is_active":false}}`, Object{"last_name": "Changed", "staff_group_ids": []interface{}{json.Number("21")}}},
		{"softphone", `{"id":11,"last_name":"Fixture","login":"PRIVATE_LOGIN","password":"PRIVATE_PASSWORD","api_token":"PRIVATE_TOKEN","app_role_id":51,"va_settings":{"is_active":true,"phones":[{"id":90,"phone":"sip:untouched@example.invalid","priority":1}],"is_need_hide_numbers":false,"is_click_to_call_auto_answer_enabled":false,"is_out_calls_auto_answer_enabled":false},"omni_settings":{"is_active":false}}`, Object{"is_need_hide_numbers": true, "is_click_to_call_auto_answer_enabled": true, "is_out_calls_auto_answer_enabled": true}},
		{"sip-lines", `{"id":11,"employee_id":12,"is_registered":false,"state":"active","channels_count":1,"is_multiterminal":false,"password":"PRIVATE_PASSWORD","api_token":"PRIVATE_TOKEN","login":"PRIVATE_LOGIN","virtual_phone_usage_rule":"fixed","virtual_phone_id":55,"pure_ani_phone_list_id":null,"sip_line_auto_choose_virtual_phone":{"is_active":false,"sip_line_virtual_phone_choose_rules":[]},"sip_line_virtual_phone_permission":{"is_active":true,"virtual_phone_ids":[55]}}`, Object{"state": "manual_lock", "channels_count": json.Number("2"), "is_multiterminal": true}},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			if tc.resource != "sip-lines" {
				tc.body = staffEmployeeFixture(t, tc.body)
			}
			var before, saved Object
			for _, target := range []*Object{&before, &saved} {
				d := json.NewDecoder(strings.NewReader(tc.body))
				d.UseNumber()
				if err := d.Decode(target); err != nil {
					t.Fatal(err)
				}
			}
			var sent Object
			reads, writes, verified := 0, 0, false
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !verified {
					t.Error("request preceded principal preflight")
				}
				switch r.Method {
				case http.MethodGet:
					reads++
					json.NewEncoder(w).Encode(saved)
				case http.MethodPut:
					writes++
					d := json.NewDecoder(r.Body)
					d.UseNumber()
					if err := d.Decode(&sent); err != nil {
						t.Error(err)
					}
					// The server stores editor fields while retaining omitted account metadata.
					for k, v := range sent {
						saved[k] = v
					}
					io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(500)
				}
			})
			c.Verify = func(context.Context, Session) error { verified = true; return nil }
			out, err := c.StaffUpdate(context.Background(), tc.resource, "11", tc.patch)
			if err != nil {
				t.Fatal(err)
			}
			if reads != 2 || writes != 1 {
				t.Fatalf("must read fresh, send once, read back: reads=%d writes=%d", reads, writes)
			}
			for _, forbidden := range []string{"password", "login", "api_token", "app_role_id", "is_token_active", "is_sip_registered", "is_registered", "full_name"} {
				if _, exists := sent[forbidden]; exists {
					t.Errorf("request included auth, rights or computed field %s", forbidden)
				}
			}
			sentBytes, _ := json.Marshal(sent)
			for _, secret := range []string{"PRIVATE_PASSWORD", "PRIVATE_LOGIN", "PRIVATE_TOKEN"} {
				if strings.Contains(string(sentBytes), secret) {
					t.Errorf("request leaked %s", secret)
				}
			}
			if tc.resource == "sip-lines" {
				for _, key := range []string{"employee_id", "virtual_phone_usage_rule", "virtual_phone_id", "pure_ani_phone_list_id", "sip_line_auto_choose_virtual_phone", "sip_line_virtual_phone_permission"} {
					if !reflect.DeepEqual(sent[key], before[key]) {
						t.Errorf("SIP editor discarded caller-ID/ownership field %s", key)
					}
				}
				if sent["is_send_info_to_mail"] != false {
					t.Fatal("SIP update could email credentials")
				}
			} else {
				va := sent["va_settings"].(map[string]interface{})
				oldVA := before["va_settings"].(map[string]interface{})
				if !reflect.DeepEqual(va["phones"], oldVA["phones"]) || va["is_active"] != true || !reflect.DeepEqual(sent["omni_settings"], before["omni_settings"]) {
					t.Fatal("editor lost phone or license blocks")
				}
			}
			target := sent
			if tc.resource == "softphone" {
				target = sent["va_settings"].(map[string]interface{})
			}
			for key, value := range tc.patch {
				if !reflect.DeepEqual(target[key], value) {
					t.Errorf("patch field not applied: %s", key)
				}
			}
			result := out["data"].(Object)
			if result["verified"] != true || result["changed"] != true {
				t.Fatal("missing verified result")
			}
			b, _ := json.Marshal(out)
			for _, secret := range []string{"PRIVATE_PASSWORD", "PRIVATE_LOGIN", "PRIVATE_TOKEN", "untouched@example.invalid"} {
				if strings.Contains(string(b), secret) {
					t.Fatal("write result leaked raw DTO")
				}
			}
		})
	}
}

// Even a confirmed requested effect is outcome_unknown if the provider changed
// untouched rights, login, phone/license settings, SIP owner or caller-ID rules.
func TestStaffUpdateDetectsCollateralChangesWithoutRetry(t *testing.T) {
	for _, tc := range []struct{ name, resource, corrupt string }{
		{"employee rights", "employees", "app_role_id"},
		{"employee login", "employees", "login"},
		{"employee phone block", "employees", "phones"},
		{"softphone phone block", "softphone", "phones"},
		{"SIP ownership", "sip-lines", "employee_id"},
		{"SIP password", "sip-lines", "password"},
		{"SIP caller-ID", "sip-lines", "virtual_phone_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := staffEmployeeFixture(t, `{"id":11,"last_name":"Before","login":"PRIVATE_LOGIN","app_role_id":51,"va_settings":{"is_active":true,"phones":[{"id":90,"phone":"sip:untouched@example.invalid"}]},"omni_settings":{"is_active":false}}`)
			patch := Object{"last_name": "After"}
			if tc.resource == "softphone" {
				patch = Object{"is_need_hide_numbers": true}
			}
			if tc.resource == "sip-lines" {
				body = `{"id":11,"employee_id":12,"is_registered":false,"state":"active","channels_count":1,"password":"PRIVATE_PASSWORD","virtual_phone_id":55}`
				patch = Object{"channels_count": json.Number("2")}
			}
			var saved Object
			d := json.NewDecoder(strings.NewReader(body))
			d.UseNumber()
			if err := d.Decode(&saved); err != nil {
				t.Fatal(err)
			}
			writes, reads := 0, 0
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					writes++
					var sent Object
					d := json.NewDecoder(r.Body)
					d.UseNumber()
					if err := d.Decode(&sent); err != nil {
						t.Error(err)
					}
					for k, v := range sent {
						saved[k] = v
					}
					if tc.corrupt == "phones" {
						delete(saved["va_settings"].(map[string]interface{}), "phones")
					} else {
						saved[tc.corrupt] = json.Number("999")
					}
					io.WriteString(w, `{}`)
					return
				}
				reads++
				json.NewEncoder(w).Encode(saved)
			})
			out, err := c.StaffUpdate(context.Background(), tc.resource, "11", patch)
			assertStaffCode(t, err, 9)
			if writes != 1 || reads != 2 {
				t.Fatalf("collateral mismatch caused replay or omitted readback: writes=%d reads=%d", writes, reads)
			}
			b, _ := json.Marshal(out)
			for _, secret := range []string{"PRIVATE_LOGIN", "PRIVATE_PASSWORD", "untouched@example.invalid"} {
				if strings.Contains(string(b), secret) || strings.Contains(err.Error(), secret) {
					t.Fatal("unresolved mutation leaked account material")
				}
			}
		})
	}
}

func TestStaffUpdateReconcilesAmbiguousDispatchOnce(t *testing.T) {
	for _, mode := range []string{"acknowledged", "server error", "connection lost", "mismatch", "readback unavailable", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			reads, writes := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					writes++
					switch mode {
					case "server error":
						w.WriteHeader(503)
					case "connection lost":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							conn.Close()
						}
					case "cancelled":
						cancel()
						w.WriteHeader(503)
					default:
						io.WriteString(w, `{}`)
					}
					return
				}
				reads++
				if reads > 1 && mode == "readback unavailable" {
					w.WriteHeader(503)
					return
				}
				surname := "Before"
				if writes > 0 && mode != "mismatch" {
					surname = "After"
				}
				io.WriteString(w, staffEmployeeFixture(t, `{"id":11,"last_name":"`+surname+`"}`))
			})
			out, err := c.StaffUpdate(ctx, "employees", "11", Object{"last_name": "After"})
			if mode == "mismatch" || mode == "readback unavailable" {
				assertStaffCode(t, err, 9)
			} else if err != nil {
				t.Fatal(err)
			} else if out["data"].(Object)["verified"] != true {
				t.Fatal("missing verified effect")
			}
			if writes != 1 || reads != 2 {
				t.Fatalf("ambiguous request replayed or lacked readback: reads=%d writes=%d", reads, writes)
			}
		})
	}
}

func TestStaffUpdateRequiresPreflightAndActiveSoftphone(t *testing.T) {
	calls := 0
	c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, staffEmployeeFixture(t, `{"id":11,"va_settings":{"is_active":false}}`))
	})
	c.Verify = nil
	_, err := c.StaffUpdate(context.Background(), "employees", "11", Object{"last_name": "After"})
	assertStaffCode(t, err, 7)
	if calls != 0 {
		t.Fatal("missing preflight reached network")
	}
	c.Verify = func(context.Context, Session) error { return &Error{Code: 4, Stage: "fixture_permission"} }
	_, err = c.StaffUpdate(context.Background(), "employees", "11", Object{"last_name": "After"})
	assertStaffCode(t, err, 4)
	if calls != 0 {
		t.Fatal("failed preflight reached target")
	}
	c.Verify = func(context.Context, Session) error { return nil }
	_, err = c.StaffUpdate(context.Background(), "softphone", "11", Object{"is_need_hide_numbers": true})
	assertStaffCode(t, err, 2)
	if calls != 1 {
		t.Fatal("inactive license must read card but never write")
	}
}

// The detail endpoint can have null summary status even though the SIP catalog
// supplies a definitive registration state. Preserve unknowns and join by ID.
func TestStaffEmployeeDetailJoinsSIPWithoutInventingStatus(t *testing.T) {
	card := staffEmployeeFixture(t, `{"id":11,"is_sip_registered":null,"status_name":null}`)
	c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/common/employee-staff/11":
			io.WriteString(w, card)
		case "/pbx/employee-sip-line":
			io.WriteString(w, `[{"id":31,"employee_id":11,"is_registered":false,"state":"active"},{"id":32,"employee_id":12,"is_registered":true,"state":"active"}]`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	out, err := c.StaffGet(context.Background(), "employees", "11")
	if err != nil {
		t.Fatal(err)
	}
	data := out["data"].(Object)
	if value, exists := data["is_sip_registered"]; !exists || value != nil {
		t.Fatal("null employee summary was replaced by an inferred boolean")
	}
	if value, exists := data["status_name"]; !exists || value != nil {
		t.Fatal("null employee status was invented")
	}
	lines := data["sip_lines"].([]Object)
	if len(lines) != 1 || ID(lines[0]) != "31" || lines[0]["is_registered"] != false {
		t.Fatal("SIP catalog state missing or joined to wrong employee")
	}
}

func TestStaffUnchangedPatchIsVerifiedWithoutWrite(t *testing.T) {
	writes, reads := 0, 0
	card := staffEmployeeFixture(t, `{"id":11,"last_name":"Fixture"}`)
	c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		reads++
		io.WriteString(w, card)
	})
	out, err := c.StaffUpdate(context.Background(), "employees", "11", Object{"last_name": "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	data := out["data"].(Object)
	if data["changed"] != false || data["verified"] != true || writes != 0 || reads != 1 {
		t.Fatal("same-value patch dispatched unnecessary write")
	}
}

func TestStaffUnknownWritableBlockDriftBlocksBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		resource, body string
		patch          Object
	}{
		{"employees", staffEmployeeFixture(t, `{"id":11,"last_name":"Before","va_settings":{"is_active":true,"unknown_new_setting":"KEEP"}}`), Object{"last_name": "After"}},
		{"softphone", staffEmployeeFixture(t, `{"id":11,"va_settings":{"is_active":true,"phones":[{"id":90,"unknown_new_phone_setting":"KEEP"}]}}`), Object{"is_need_hide_numbers": true}},
		{"sip-lines", `{"id":11,"state":"active","is_registered":false,"channels_count":1,"sip_line_virtual_phone_permission":{"is_active":true,"unknown_new_rule":"KEEP"}}`, Object{"channels_count": json.Number("2")}},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			writes := 0
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				io.WriteString(w, tc.body)
			})
			_, err := c.StaffUpdate(context.Background(), tc.resource, "11", tc.patch)
			assertStaffCode(t, err, 7)
			if writes != 0 {
				t.Fatal("unknown writable block drift dispatched a destructive partial editor DTO")
			}
		})
	}
}

// Group membership is a set: provider ordering and null-for-empty encoding do
// not turn a confirmed save into outcome_unknown or an unnecessary mutation.
func TestStaffGroupMembershipNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, before, patch, after string
		writes                     int
	}{
		{"reordered readback", `[21]`, `[21,22]`, `[22,21]`, 1},
		{"clear readback null", `[21]`, `[]`, `null`, 1},
		{"reordered no-op", `[22,21]`, `[21,22]`, `[22,21]`, 0},
		{"empty null no-op", `null`, `[]`, `null`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := staffEmployeeFixture(t, `{"id":11,"staff_group_ids":`+tc.before+`}`)
			after := staffEmployeeFixture(t, `{"id":11,"staff_group_ids":`+tc.after+`}`)
			var patch Object
			d := json.NewDecoder(strings.NewReader(`{"staff_group_ids":` + tc.patch + `}`))
			d.UseNumber()
			if err := d.Decode(&patch); err != nil {
				t.Fatal(err)
			}
			reads, writes := 0, 0
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					writes++
					io.WriteString(w, `{}`)
					return
				}
				reads++
				if writes > 0 {
					io.WriteString(w, after)
				} else {
					io.WriteString(w, before)
				}
			})
			out, err := c.StaffUpdate(context.Background(), "employees", "11", patch)
			if err != nil {
				t.Fatal(err)
			}
			data := out["data"].(Object)
			if data["verified"] != true || data["changed"] != (tc.writes == 1) {
				t.Fatal("wrong normalized membership result")
			}
			if writes != tc.writes || reads != 1+tc.writes {
				t.Fatalf("membership normalization dispatched/reconciled incorrectly: reads=%d writes=%d", reads, writes)
			}
		})
	}
}

func TestStaffNumericStringPatchesRejectedBeforeIO(t *testing.T) {
	for _, tc := range []struct{ resource, body string }{
		{"employees", `{"schedule_id":"21"}`},
		{"employees", `{"main_staff_group_id":"21"}`},
		{"employees", `{"staff_group_ids":["21"]}`},
		{"employees", `{"staff_group_ids":[21,"22"]}`},
		{"sip-lines", `{"channels_count":"2"}`},
	} {
		t.Run(tc.body, func(t *testing.T) {
			var patch Object
			d := json.NewDecoder(strings.NewReader(tc.body))
			d.UseNumber()
			if err := d.Decode(&patch); err != nil {
				t.Fatal(err)
			}
			calls := 0
			c := staffFixtureClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
			c.Verify = func(context.Context, Session) error { calls++; return nil }
			_, err := c.StaffUpdate(context.Background(), tc.resource, "11", patch)
			assertStaffCode(t, err, 2)
			if calls != 0 {
				t.Fatal("numeric-string patch reached preflight or provider")
			}
		})
	}
}
