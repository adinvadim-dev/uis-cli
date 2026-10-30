package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAutodialCreateValidBodyAndIdentity(t *testing.T) {
	created := false
	writes := 0
	var saved Object
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2.0":
			io.WriteString(w, `{"id":"uis-autodial-account","result":{"data":[{"timezone":"Europe/Moscow"}],"metadata":{}}}`)
		case "/available_number_capacity_usage_rule_list":
			io.WriteString(w, `[{"id":"fixed","name":"Fixed"}]`)
		case "/number_capacity":
			io.WriteString(w, `[{"id":7,"numb":"70000000001"}]`)
		case "/auto_call/employees_in_groups":
			io.WriteString(w, `[{"group_id":1,"employees":[{"id":8,"full_name":"Fixture"}]}]`)
		case "/auto_call/campaign/validate_numbers":
			io.WriteString(w, `{"numbers":[{"numa":"70000000000","name":"Fixture","comment":null,"validation_error":null}]}`)
		case "/auto_call/campaign_list":
			if !created {
				io.WriteString(w, `[]`)
			} else {
				io.WriteString(w, `[{"id":123,"name":"uis-cli test fixture"}]`)
			}
		case "/auto_call/campaign":
			if r.Method == "POST" {
				writes++
				d := json.NewDecoder(r.Body)
				d.UseNumber()
				if d.Decode(&saved) != nil {
					t.Fatal("bad create body")
				}
				if saved["number_capacity_usage_rule"] != "fixed" || saved["number_capacity_id"] != json.Number("7") {
					t.Fatal("caller defaults lost")
				}
				created = true
				w.WriteHeader(200)
				return
			}
			out := Object{}
			for k, v := range saved {
				out[k] = v
			}
			out["id"] = 123
			out["status"] = "planned"
			out["begin_date"] = "2099-01-01T07:00:00+00:00"
			out["subscriber_time_from"] = "10:00:00"
			out["subscriber_time_till"] = "20:00:00"
			out["phone_list"] = []Object{{"numa": "70000000000", "name": "Fixture", "comment": nil, "status_mnemonic": "not_processed"}}
			json.NewEncoder(w).Encode(out)
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	c.RPCURL = s.URL
	c.Verify = func(context.Context, Session) error { return nil }
	v, e := c.Autodial(context.Background(), "create", "", Object{"name": "uis-cli test fixture", "phone_list": []Object{{"numa": "70000000000", "name": "Fixture"}}, "employee_ids": []int{8}, "begin_date": "2099-01-01 10:00:00", "number_capacity_usage_rule": "fixed", "number_capacity_id": 7})
	if e != nil {
		t.Fatal(e)
	}
	o := v.(map[string]interface{})
	if writes != 1 || o["verified"] != true || o["campaign_id"] != json.Number("123") {
		t.Fatal("create not reconciled to provider identity")
	}
}

func TestAutodialCampaignRejectsInvalidBeforeNetwork(t *testing.T) {
	for _, p := range []Object{
		{}, {"status": "draft"}, {"employee_ids": []int{}}, {"phone_list": []Object{{"numa": ""}}}, {"begin_date": "tomorrow"}, {"subscriber_time_from": "27:00"}, {"attempts": Object{"other": []string{"bad"}}}, {"subscriber_playlist": Object{"items": []Object{{"scenario_id": 1}}}},
	} {
		c := New(Session{}, time.Second)
		_, e := c.Autodial(context.Background(), "create", "", p)
		var pe *Error
		if !errors.As(e, &pe) || pe.Code != 2 {
			t.Fatalf("invalid payload reached auth: %v", e)
		}
	}
}
func TestAutodialPlannedSettingsNeverDispatch(t *testing.T) {
	writes := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2.0" {
			io.WriteString(w, `{"id":"uis-autodial-account","result":{"data":[{"timezone":"Europe/Moscow"}]}}`)
			return
		}
		if r.Method != "GET" {
			writes++
		}
		io.WriteString(w, `{"id":123,"status":"planned"}`)
	}))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	c.RPCURL = s.URL
	c.Verify = func(context.Context, Session) error { return nil }
	_, e := c.Autodial(context.Background(), "update", "123", Object{"begin_date": "2099-01-02 10:00:00"})
	var pe *Error
	if !errors.As(e, &pe) || pe.Stage != "campaign_setting_readonly_after_creation" || writes != 0 {
		t.Fatalf("ignored setting sent: %v", e)
	}
}

func TestAutodialUpdateEffectsAndPartialFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		put, remove int
		ignore      bool
		code        int
	}{{"verified", 200, 200, false, 0}, {"ignored", 200, 200, true, 9}, {"reconcile server error", 503, 200, false, 0}, {"partial definite failure", 400, 200, true, 9}, {"removal uncertain", 200, 503, false, 9}, {"unexpected activation", 200, 200, false, 9}} {
		t.Run(tc.name, func(t *testing.T) {
			puts, removes := 0, 0
			removed, saved := false, false
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2.0":
					var p Object
					json.NewDecoder(r.Body).Decode(&p)
					switch p["method"] {
					case "get.account":
						io.WriteString(w, `{"id":"uis-autodial-account","result":{"data":[{"timezone":"UTC"}]}}`)
					case "delete.auto_call_campaign_phone_numbers":
						removes++
						if tc.remove == 200 {
							removed = true
						}
						w.WriteHeader(tc.remove)
						io.WriteString(w, `{"id":"uis-autodial","result":{"data":{},"metadata":{}}}`)
					case "get.auto_call_campaigns":
						io.WriteString(w, `{"id":"uis-autodial","result":{"data":[{"id":123,"status":"planned","phone_numbers":[{"phone_number":"70000000000","status":"deleted"}]}]}}`)
					default:
						t.Error("unexpected RPC")
					}
				case "/auto_call/employees_in_groups":
					io.WriteString(w, `[{"employees":[{"id":8},{"id":9}]}]`)
				case "/number_capacity":
					io.WriteString(w, `[{"id":7}]`)
				case "/auto_call/campaign/validate_numbers":
					io.WriteString(w, `{"numbers":[{"numa":"70000000001","name":"New","comment":null,"validation_error":null}]}`)
				case "/auto_call/campaign":
					if r.Method == "PUT" {
						puts++
						var p Object
						json.NewDecoder(r.Body).Decode(&p)
						if !removed || p["begin_date"] != "2099-01-01 10:00:00" || p["status"] != nil {
							t.Error("replacement did not retain settings / separate status")
						}
						saved = !tc.ignore
						w.WriteHeader(tc.put)
						return
					}
					phone, employee := "70000000000", 8
					name := "Old"
					if saved {
						phone, employee, name = "70000000001", 9, "New"
					}
					status := "planned"
					if saved && tc.name == "unexpected activation" {
						status = "in_process"
					}
					json.NewEncoder(w).Encode(Object{"id": 123, "name": "Owned fixture", "status": status, "begin_date": "2099-01-01T10:00:00Z", "number_capacity_usage_rule": "fixed", "number_capacity_id": 7, "employee_ids": []int{employee}, "phone_list": []Object{{"numa": phone, "name": name, "comment": nil, "status_mnemonic": "not_processed"}}})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer s.Close()
			c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
			c.SupervisorURL = s.URL
			c.RPCURL = s.URL
			c.Verify = func(context.Context, Session) error { return nil }
			v, e := c.Autodial(context.Background(), "update", "123", Object{"employee_ids": []int{9}, "phone_list": []Object{{"numa": "70000000001", "name": "New"}}})
			if removes != 1 || puts > 1 {
				t.Fatal("mutation replayed")
			}
			if tc.remove == 503 && puts != 0 {
				t.Fatal("uncertain removal followed by PUT")
			}
			if tc.code == 0 {
				if e != nil || v.(map[string]interface{})["verified"] != true {
					t.Fatalf("update not verified: %v", e)
				}
			} else {
				var pe *Error
				if !errors.As(e, &pe) || pe.Code != tc.code || pe.CampaignID != "123" {
					t.Fatalf("unsafe update error: %v", e)
				}
			}
		})
	}
}

func TestAutodialCreateWithoutIdentityNeverRetries(t *testing.T) {
	for _, status := range []int{200, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			writes, lists := 0, 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2.0":
					io.WriteString(w, `{"id":"uis-autodial-account","result":{"data":[{"timezone":"UTC"}]}}`)
				case "/auto_call/employees_in_groups":
					io.WriteString(w, `[{"employees":[{"id":8}]}]`)
				case "/number_capacity":
					io.WriteString(w, `[{"id":7}]`)
				case "/auto_call/campaign/validate_numbers":
					io.WriteString(w, `{"numbers":[{"numa":"70000000000","validation_error":null}]}`)
				case "/auto_call/campaign_list":
					lists++
					io.WriteString(w, `[]`)
				case "/auto_call/campaign":
					writes++
					w.WriteHeader(status)
				default:
					t.Error("unexpected endpoint")
				}
			}))
			defer s.Close()
			c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
			c.SupervisorURL = s.URL
			c.RPCURL = s.URL
			c.Verify = func(context.Context, Session) error { return nil }
			_, e := c.Autodial(context.Background(), "create", "", Object{"name": "uis-cli test", "begin_date": "2099-01-01 10:00:00", "employee_ids": []int{8}, "phone_list": []Object{{"numa": "70000000000"}}, "number_capacity_usage_rule": "fixed", "number_capacity_id": 7})
			var pe *Error
			if !errors.As(e, &pe) || pe.Code != 9 || pe.Stage != "campaign_create_identity" || writes != 1 || lists != 2 {
				t.Fatalf("unresolved create replayed or accepted: %v", e)
			}
		})
	}
}

func TestAutodialPlaylistRejectsEmptyOrOversizedTTS(t *testing.T) {
	for _, text := range []string{"", " \n\t ", strings.Repeat("Я", 1001), strings.Repeat("Я", 1000) + " "} {
		e := ValidateCampaignInput("update", Object{"subscriber_playlist": Object{"items": []Object{{"tts_string": text}}}})
		var pe *Error
		if !errors.As(e, &pe) || pe.Stage != "campaign_tts_text" {
			t.Fatalf("invalid TTS reached dispatch: %v", e)
		}
	}
	if e := ValidateCampaignInput("update", Object{"subscriber_playlist": Object{"items": []Object{{"tts_string": "Тест"}}}}); e != nil {
		t.Fatal(e)
	}
}

func TestAutodialUpdateRejectsNullBeginDate(t *testing.T) {
	e := ValidateCampaignInput("update", Object{"begin_date": nil})
	var pe *Error
	if !errors.As(e, &pe) || pe.Code != 2 || pe.Stage != "campaign_begin_date_required" {
		t.Fatalf("null begin_date must fail locally, got %v", e)
	}
}
