package cli

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"uis-cli/internal/archive"
	"uis-cli/internal/client"
	"uis-cli/internal/portal"
	"uis-cli/internal/profile"
)

func profileFor(g *globalFlags) (string, profile.Account, error) {
	path := profileConfigPath(g)
	if strings.HasSuffix(path, ".json") {
		return profile.Select(profile.Config{}, g.Account)
	}
	c, e := profile.Load(path)
	if e != nil {
		return "", profile.Account{}, exitf(2, "Invalid profile config")
	}
	return profile.Select(c, g.Account)
}
func confirm(g *globalFlags, plan interface{}) error {
	if g.Force {
		return nil
	}
	if g.NoInput || !isatty.IsTerminal(os.Stdin.Fd()) {
		return exitf(10, "Confirmation required: review --dry-run then use --force")
	}
	b, _ := json.Marshal(plan)
	fmt.Fprintln(os.Stderr, string(b))
	fmt.Fprint(os.Stderr, "Send once? Type yes: ")
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(s) != "yes" {
		return exitf(10, "Cancelled")
	}
	return nil
}
func operationError(e error) error {
	if e == nil {
		return nil
	}
	var pe *portal.Error
	if errors.As(e, &pe) {
		return &ExitError{Code: pe.Code, Msg: pe.Error(), Details: map[string]interface{}{"stage": pe.Stage, "ticket_id": pe.TicketID}}
	}
	if errors.Is(e, sql.ErrNoRows) {
		return exitf(5, "Not found in local archive")
	}
	return e
}
func privateClient(g *globalFlags) (*portal.Client, error) {
	name, account, e := profileFor(g)
	if e != nil {
		return nil, e
	}
	s, e := portal.LoadSession(profile.State(name))
	if e != nil {
		return nil, exitf(3, "Private session unavailable; run auth login")
	}
	if portal.NormalizeLogin(account.ExpectedLogin) == "" || portal.NormalizeLogin(s.Login) != portal.NormalizeLogin(account.ExpectedLogin) || (account.ExpectedCustomerID != "" && s.CustomerID != account.ExpectedCustomerID) {
		return nil, exitf(3, "Private session principal does not match account binding")
	}
	to, e := timeout(g)
	if e != nil {
		return nil, e
	}
	c := portal.New(s, to)
	// Mutations re-probe an authenticated support read before dispatch.
	// Reads do not call Verify. No separate CSRF token was established for
	// infopin Bearer writes; session drift is caught by this probe.
	c.Verify = func(ctx context.Context, session portal.Session) error {
		if portal.NormalizeLogin(session.Login) != portal.NormalizeLogin(account.ExpectedLogin) {
			return &portal.Error{Code: 3, Stage: "principal_mismatch"}
		}
		probe := portal.New(session, to)
		probe.BaseURL = c.BaseURL
		probe.AppURL = c.AppURL
		probe.HTTP = c.HTTP
		return probe.ProbeAuthenticatedRead(ctx)
	}
	return c, nil
}
func timeout(g *globalFlags) (time.Duration, error) {
	v := g.Timeout
	if v == "" {
		v = os.Getenv("UIS_TIMEOUT")
	}
	if v == "" {
		v = "30s"
	}
	d, e := time.ParseDuration(v)
	if e != nil || d <= 0 || d > 5*time.Minute {
		return 0, exitf(2, "timeout must be >0 and <=5m")
	}
	return d, nil
}
func rpc(g *globalFlags, method string, params map[string]interface{}) (json.RawMessage, error) {
	_, cfg, e := loadEffectiveConfig(g)
	if e != nil {
		return nil, e
	}
	if !client.IsRead(method) {
		if e = confirm(g, map[string]interface{}{"method": method, "params": redactParams(params)}); e != nil {
			return nil, e
		}
	}
	if cfg.AccessToken == "" {
		_, a, e := profileFor(g)
		if e != nil {
			return nil, e
		}
		fields, e := profile.Resolve(context.Background(), a, "dataapi.access_token")
		if e != nil {
			return nil, exitf(3, "%s", e)
		}
		cfg.AccessToken = fields["dataapi.access_token"]
	}
	params["access_token"] = cfg.AccessToken
	c, e := newClient(cfg)
	if e != nil {
		return nil, e
	}
	ctx, cancel, e := ctxWithTimeout(cfg)
	if e != nil {
		return nil, e
	}
	defer cancel()
	resp, st, _, e := c.Call(ctx, method, params)
	if e != nil {
		code := 6
		if errors.Is(e, client.ErrOutcomeUnknown) {
			code = 9
		} else {
			switch st {
			case 401:
				code = 3
			case 403:
				code = 4
			case 429:
				code = 8
			}
		}
		return nil, exitf(code, "%s request failed; provider body suppressed", method)
	}
	if resp.Error != nil {
		return nil, exitf(rpcErrorCode(resp.Error, client.IsRead(method)), "%s rejected or unresolved; provider body suppressed", method)
	}

	return resp.Result, nil
}
func runRPC(g *globalFlags, method string, p map[string]interface{}) error {
	if g.DryRun {
		return writeDryRunRequest(g, method, p)
	}
	v, e := rpc(g, method, p)
	if e != nil {
		return e
	}
	if method == "get.calls_report" || method == "get.call_legs_report" {
		var data interface{}
		d := json.NewDecoder(strings.NewReader(string(v)))
		d.UseNumber()
		if d.Decode(&data) != nil {
			return exitf(7, "Invalid call report")
		}
		return emit(g, archive.Scrub(data))
	}
	return writeResult(g, v)
}
func emit(g *globalFlags, v interface{}) error {
	if g.JSON {
		return writeJSON(g, v)
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return writeResult(g, b)
}
// pageObjects applies optional status/query filters and limit/page locally.
// Used for live support lists because provider offset/limit/status query params 500.
func pageObjects(rows []portal.Object, status, query string, limit, page int) []portal.Object {
	out := []portal.Object{}
	skip := (page - 1) * limit
	q := strings.ToLower(query)
	for _, o := range rows {
		if status != "" && fmt.Sprint(o["status"]) != status {
			continue
		}
		if q != "" {
			b, _ := json.Marshal(o)
			if !strings.Contains(strings.ToLower(string(b)), q) {
				continue
			}
		}
		if skip > 0 {
			skip--
			continue
		}
		if len(out) < limit {
			out = append(out, o)
		}
	}
	return out
}

func positive(v string) (json.Number, error) {
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil || n <= 0 {
		return "", exitf(2, "ID must be a positive integer")
	}
	return json.Number(v), nil
}
func bodyInput(cmd *cobra.Command) (string, error) {
	body, _ := cmd.Flags().GetString("body")
	file, _ := cmd.Flags().GetString("file")
	if cmd.Flags().Changed("body") == cmd.Flags().Changed("file") {
		return "", exitf(2, "Use exactly one of --body or --file (use --file - for stdin)")
	}
	if file != "" {
		var b []byte
		var e error
		if file == "-" {
			b, e = io.ReadAll(io.LimitReader(os.Stdin, 2049))
		} else {
			f, err := os.Open(file)
			if err != nil {
				return "", exitf(2, "Cannot open message file")
			}
			defer f.Close()
			b, e = io.ReadAll(io.LimitReader(f, 2049))
		}
		if e != nil {
			return "", exitf(2, "Cannot read message")
		}
		body = string(b)
	}
	if strings.TrimSpace(body) == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > 500 {
		return "", exitf(2, "Message must contain 1–500 characters")
	}
	return body, nil
}
func addBody(c *cobra.Command) {
	c.Flags().String("body", "", "Message text")
	c.Flags().String("file", "", "Message file (- for stdin)")
}
func newCalls(g *globalFlags) *cobra.Command {
	root := &cobra.Command{Use: "calls", Short: "Live call search, details and annotations"}
	for _, action := range []string{"search", "show", "legs", "comment", "tag"} {
		action := action
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			p := map[string]interface{}{}
			from, _ := cmd.Flags().GetString("from")
			till, _ := cmd.Flags().GetString("till")
			if from == "" {
				from = time.Now().AddDate(0, 0, -90).Format("2006-01-02 15:04:05")
			}
			if till == "" {
				till = time.Now().Format("2006-01-02 15:04:05")
			}
			hint, _ := cmd.Flags().GetString("date-hint")
			if hint != "" {
				d, e := time.Parse("2006-01-02", hint)
				if e != nil {
					return exitf(2, "date-hint must be YYYY-MM-DD")
				}
				from = d.AddDate(0, 0, -1).Format("2006-01-02 15:04:05")
				till = d.AddDate(0, 0, 1).Format("2006-01-02 15:04:05")
			}
			df, e := time.Parse("2006-01-02 15:04:05", from)
			if e != nil {
				return exitf(2, "from must be YYYY-MM-DD HH:MM:SS")
			}
			dt, e := time.Parse("2006-01-02 15:04:05", till)
			if e != nil || !df.Before(dt) {
				return exitf(2, "till must be after from")
			}
			p["date_from"] = from
			p["date_till"] = till
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			if limit < 1 || limit > 10000 || offset < 0 {
				return exitf(2, "Invalid limit or offset")
			}
			if action == "show" {
				limit = 1
			}
			p["limit"] = limit
			p["offset"] = offset
			filters := []interface{}{}
			filter := func(field string, v interface{}) {
				filters = append(filters, map[string]interface{}{"field": field, "operator": "=", "value": v})
			}
			if len(args) > 0 {
				id, e := positive(args[0])
				if e != nil {
					return e
				}
				field := "id"
				if action == "legs" {
					field = "call_session_id"
				}
				filter(field, id)
			}
			for flag, field := range map[string]string{"id": "id", "phone": "contact_phone_number", "direction": "direction", "employee-id": "employee_id", "virtual-number": "virtual_phone_number", "scenario-id": "scenario_id"} {
				v, _ := cmd.Flags().GetString(flag)
				if v != "" {
					var val interface{} = v
					if flag == "id" || flag == "employee-id" || flag == "scenario-id" {
						n, e := positive(v)
						if e != nil {
							return e
						}
						val = n
					}
					if flag == "direction" && v != "in" && v != "out" {
						return exitf(2, "direction must be in or out")
					}
					if flag == "employee-id" && action != "legs" {
						filters = append(filters, map[string]interface{}{"field": "employees", "operator": "jsquery", "value": "#.employee_id = " + v})
					} else {
						filter(field, val)
					}
				}
			}
			for _, flag := range []string{"lost", "transfer"} {
				if cmd.Flags().Changed(flag) {
					v, _ := cmd.Flags().GetBool(flag)
					field := "is_lost"
					if flag == "transfer" {
						field = "is_transfer"
						if action == "legs" {
							field = "is_transfered"
						}
					}
					filter(field, v)
				}
			}
			raw, _ := cmd.Flags().GetString("filter-json")
			if raw != "" {
				var f interface{}
				if json.Unmarshal([]byte(raw), &f) != nil {
					return exitf(2, "Invalid filter-json")
				}
				if _, ok := f.(map[string]interface{}); !ok {
					return exitf(2, "filter-json must be an object")
				}
				filters = append(filters, f)
			}
			if len(filters) == 1 {
				p["filter"] = filters[0]
			} else if len(filters) > 1 {
				p["filter"] = map[string]interface{}{"filters": filters, "condition": "and"}
			}
			method := "get.calls_report"
			if action == "legs" {
				method = "get.call_legs_report"
			}
			if action == "comment" || action == "tag" {
				id, _ := positive(args[0])
				p = map[string]interface{}{"communication_id": id, "communication_type": "call"}
				if action == "comment" {
					b, e := bodyInput(cmd)
					if e != nil {
						return e
					}
					p["comment"] = b
					method = "create.communication_comment"
				} else {
					add, _ := cmd.Flags().GetString("add")
					remove, _ := cmd.Flags().GetString("remove")
					if (add == "") == (remove == "") {
						return exitf(2, "Use exactly one of --add or --remove")
					}
					method = "set.tag_communications"
					if remove != "" {
						method = "unset.tag_communications"
						add = remove
					}
					n, e := positive(add)
					if e != nil {
						return e
					}
					p["tag_id"] = n
				}
			}
			if action == "comment" || action == "tag" {
				return annotateCall(g, method, p, args[0], from, till)
			}
			if action == "show" && !g.DryRun {
				reason := "private_session_unavailable"
				pc, e := privateClient(g)
				if e == nil {
					v, err := pc.Call(context.Background(), args[0])
					if err == nil {
						return emit(g, map[string]interface{}{"source": "private.call_details_data", "data": archive.Scrub(v)})
					}
					reason = "private_details_unavailable"
				}
				v, e := rpc(g, method, p)
				if e != nil {
					return e
				}
				var data interface{}
				decoder := json.NewDecoder(strings.NewReader(string(v)))
				decoder.UseNumber()
				if decoder.Decode(&data) != nil {
					return exitf(7, "Invalid report response")
				}
				rows, err := portal.Rows(data)
				if err != nil {
					return exitf(7, "Invalid report schema")
				}
				var matching portal.Object
				for _, row := range rows {
					if portal.ID(row) == args[0] {
						matching = row
						break
					}
				}
				if matching == nil {
					return exitf(5, "Call not found in the requested date window")
				}
				data = archive.Scrub(matching)
				return emit(g, map[string]interface{}{"source": "dataapi.get.calls_report", "reduced": true, "fallback_reason": reason, "data": data})
			}
			return runRPC(g, method, p)
		}}
		if action != "search" {
			c.Use += " <session-id>"
			c.Args = cobra.ExactArgs(1)
		}
		for _, flag := range []string{"from", "till", "date-hint", "id", "phone", "direction", "employee-id", "virtual-number", "scenario-id", "filter-json"} {
			if action == "legs" && flag == "phone" {
				continue
			}
			c.Flags().String(flag, "", "Call query "+flag)
		}
		c.Flags().Int("limit", 100, "Page size")
		c.Flags().Int("offset", 0, "Offset")
		if action != "legs" {
			c.Flags().Bool("lost", false, "Lost calls")
		}
		c.Flags().Bool("transfer", false, "Transferred calls")
		if action == "comment" {
			addBody(c)
		}
		if action == "tag" {
			c.Flags().String("add", "", "Tag ID")
			c.Flags().String("remove", "", "Tag ID (may remove sale attribution)")
		}
		root.AddCommand(c)
	}
	return root
}

type supportSource struct{ c *portal.Client }

func (s supportSource) Tickets(ctx context.Context, offset, limit int) ([]portal.Object, error) {
	return s.c.Tickets(ctx, offset, limit)
}
func (s supportSource) Comments(ctx context.Context, t portal.Object, offset, limit int) ([]portal.Object, error) {
	return s.c.Comments(ctx, t, offset, limit)
}
func newSupport(g *globalFlags) *cobra.Command {
	root := &cobra.Command{Use: "support", Short: "Private UIS support and offline ticket archive"}
	for _, a := range []string{"sync", "status", "doctor"} {
		a := a
		root.AddCommand(&cobra.Command{Use: a, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			name, _, e := profileFor(g)
			if e != nil {
				return e
			}
			if g.DryRun {
				return emit(g, map[string]interface{}{"account": name, "action": a, "network": a == "sync"})
			}
			s, e := archive.Open(profile.State(name))
			if e != nil {
				return e
			}
			defer s.Close()
			if a == "sync" {
				pc, e := privateClient(g)
				if e != nil {
					return e
				}
				if e = s.Sync(context.Background(), supportSource{pc}); e != nil {
					return operationError(e)
				}
			}
			v, e := s.Status()
			if e != nil {
				return e
			}
			return emit(g, v)
		}})
	}
	for _, kind := range []string{"topic", "contact"} {
		kind := kind
		r := &cobra.Command{Use: kind}
		r.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			if g.DryRun {
				return emit(g, map[string]interface{}{"resource": kind, "network": false})
			}
			pc, e := privateClient(g)
			if e != nil {
				return e
			}
			resource := "contact"
			q := url.Values{}
			if kind == "topic" {
				// Live infopin accepts availability filters; sort=order_num returns HTTP 500.
				resource = "tech-support-web-type"
				q.Set("is_available", "true")
				q.Set("is_active_customer", "true")
			}
			v, e := pc.Read(context.Background(), resource, q)
			if e != nil {
				return operationError(e)
			}
			return emit(g, archive.Scrub(v))
		}})
		root.AddCommand(r)
	}
	tickets := &cobra.Command{Use: "ticket"}
	for _, action := range []string{"list", "show", "search", "create", "reply", "close", "reopen"} {
		action := action
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			name, _, e := profileFor(g)
			if e != nil {
				return e
			}
			live, _ := cmd.Flags().GetBool("live")
			limit, _ := cmd.Flags().GetInt("limit")
			page, _ := cmd.Flags().GetInt("page")
			query, _ := cmd.Flags().GetString("query")
			status, _ := cmd.Flags().GetString("status")
			if limit < 1 || limit > 10000 || page < 1 {
				return exitf(2, "Invalid pagination")
			}
			if action == "show" || action == "reply" || action == "close" || action == "reopen" {
				if _, e = positive(args[0]); e != nil {
					return e
				}
			}
			fields := portal.Object{}
			files, _ := cmd.Flags().GetStringArray("attachment")
			message := ""
			if action == "create" || action == "reply" {
				message, e = bodyInput(cmd)
				if e != nil {
					return e
				}
			}
			if action == "create" {
				for flag, key := range map[string]string{"topic-id": "tech_support_web_type_id", "contact-id": "contact_id", "phone-id": "phone_id", "email-id": "email_id"} {
					v, _ := cmd.Flags().GetString(flag)
					n, e := positive(v)
					if e != nil {
						return exitf(2, "%s is required and must be a positive integer", flag)
					}
					fields[key] = n
				}
				fields["context"] = message
			}
			plan := map[string]interface{}{"account": name, "action": action, "attachment_count": len(files), "fields": fields}
			if len(args) > 0 {
				plan["target"] = args[0]
			}
			if action == "reply" {
				plan["message"] = message
			}
			if g.DryRun {
				return emit(g, plan)
			}
			mutation := action == "create" || action == "reply" || action == "close" || action == "reopen"
			if mutation {
				if e = confirm(g, plan); e != nil {
					return e
				}
				pc, e := privateClient(g)
				if e != nil {
					return e
				}
				var v interface{}
				switch action {
				case "create":
					v, e = pc.Create(context.Background(), fields, files)
				case "reply":
					v, e = pc.Reply(context.Background(), args[0], message, files)
				case "close":
					v, e = pc.Status(context.Background(), args[0], "closed")
				case "reopen":
					v, e = pc.Status(context.Background(), args[0], "created")
				}
				if e != nil {
					return operationError(e)
				}
				return emit(g, archive.Scrub(v))
			}
			var s *archive.Store
			var v interface{}
			if live {
				pc, e := privateClient(g)
				if e != nil {
					return e
				}
				if action == "show" {
					t, err := pc.Ticket(context.Background(), args[0])
					if err != nil {
						return operationError(err)
					}
					comments, err := pc.Comments(context.Background(), t, 0, 0)
					if err != nil {
						return operationError(err)
					}
					v = map[string]interface{}{"ticket": t, "comments": comments}
				} else {
					// Live list rejects offset/limit/status query params; _q works.
					// Fetch the working unfiltered (or _q) set and page/filter locally.
					if action == "search" {
						query = args[0]
					}
					q := url.Values{}
					if query != "" {
						q.Set("_q", query)
					}
					raw, err := pc.Read(context.Background(), "tech-support", q)
					if err != nil {
						return operationError(err)
					}
					rows, err := portal.Rows(raw)
					if err != nil {
						return operationError(err)
					}
					v = pageObjects(rows, status, "", limit, page)
				}
			} else {
				s, e = archive.Open(profile.State(name))
				if e != nil {
					return e
				}
				defer s.Close()
				switch action {
				case "show":
					v, e = s.Show(args[0])
				case "search":
					v, e = s.Search(args[0], status, limit, (page-1)*limit)
				default:
					v, e = s.List(status, query, limit, page)
				}
			}
			if e != nil {
				return operationError(e)
			}
			var meta map[string]interface{}
			if s != nil {
				meta, e = s.Status()
				if e != nil {
					return e
				}
			}
			return emit(g, map[string]interface{}{"data": archive.Scrub(v), "archive": meta, "source": map[bool]string{true: "live", false: "local"}[live]})
		}}
		if action == "show" || action == "reply" || action == "close" || action == "reopen" {
			c.Use += " <id>"
			c.Args = cobra.ExactArgs(1)
		}
		if action == "search" {
			c.Use += " <query>"
			c.Args = cobra.ExactArgs(1)
		}
		c.Flags().Bool("live", false, "Read provider instead of archive")
		c.Flags().String("status", "", "Status filter")
		c.Flags().String("query", "", "Text filter")
		c.Flags().Int("limit", 100, "Limit")
		c.Flags().Int("page", 1, "Page")
		if action == "create" || action == "reply" {
			addBody(c)
			c.Flags().StringArray("attachment", nil, "Attachment path (repeatable)")
		}
		if action == "create" {
			for _, f := range []string{"topic-id", "contact-id", "phone-id", "email-id"} {
				c.Flags().String(f, "", "Required provider identity")
			}
		}
		tickets.AddCommand(c)
	}
	root.AddCommand(tickets)
	return root
}
func newDiscovery(g *globalFlags, use string, methods map[string]string) *cobra.Command {
	r := &cobra.Command{Use: use}
	for action, method := range methods {
		method := method
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			t, _ := cmd.Flags().GetString("report-type")
			p := map[string]interface{}{}
			if use == "filters" && t == "" {
				return exitf(2, "report-type is required")
			}
			if t != "" {
				p["report_type"] = t
			}
			return runRPC(g, method, p)
		}}
		c.Flags().String("report-type", "", "Report type")
		r.AddCommand(c)
	}
	return r
}
func newAccounts(g *globalFlags) *cobra.Command {
	r := &cobra.Command{Use: "account"}
	for _, action := range []string{"add", "list", "show", "use", "remove", "doctor"} {
		action := action
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			path := profile.Path()
			if g.ConfigPath != "" {
				path = g.ConfigPath
			}
			cfg, e := profile.Load(path)
			if e != nil {
				return exitf(2, "Invalid profile config")
			}
			name, a, e := profile.Select(cfg, g.Account)
			if e != nil {
				return e
			}
			if len(args) > 0 {
				name = args[0]
				if !profile.ValidName.MatchString(name) {
					return exitf(2, "Invalid account name")
				}
				a = cfg.Accounts[name]
			}
			if g.DryRun {
				return emit(g, map[string]interface{}{"account": name, "action": action})
			}
			switch action {
			case "list":
				return emit(g, map[string]interface{}{"default_account": cfg.DefaultAccount, "accounts": cfg.Accounts})
			case "show":
				return emit(g, a)
			case "doctor":
				_, e := os.Stat(filepath.Join(profile.State(name), "session.json"))
				probe := "missing_session"
				if e == nil {
					probe = "credential_binding_and_authenticated_read"
				}
				return emit(g, map[string]interface{}{"account": name, "credential_process_configured": len(a.CredentialProcess.Command) > 0, "dataapi_env_present": os.Getenv("UIS_TOKEN") != "", "session_present": e == nil, "principal_probe": probe})
			case "add":
				if _, ok := cfg.Accounts[name]; ok {
					return exitf(2, "Account exists")
				}
				login, _ := cmd.Flags().GetString("expected-login")
				argv, _ := cmd.Flags().GetStringArray("credential-command")
				a.Alias = name
				a.ExpectedLogin = login
				a.CredentialProcess.Command = argv
				cfg.Accounts[name] = a
				if cfg.DefaultAccount == "" {
					cfg.DefaultAccount = name
				}
			case "use":
				if _, ok := cfg.Accounts[name]; !ok {
					return exitf(5, "Unknown account")
				}
				cfg.DefaultAccount = name
			case "remove":
				if e = confirm(g, map[string]interface{}{"remove_profile": name}); e != nil {
					return e
				}
				delete(cfg.Accounts, name)
				if cfg.DefaultAccount == name {
					cfg.DefaultAccount = ""
				}
			}
			if e = profile.Save(path, cfg); e != nil {
				return e
			}
			return emit(g, map[string]interface{}{"account": name, "action": action})
		}}
		if action == "add" || action == "use" || action == "remove" {
			c.Use += " <name>"
			c.Args = cobra.ExactArgs(1)
		}
		if action == "add" {
			c.Flags().String("expected-login", "", "Expected provider login")
			c.Flags().StringArray("credential-command", nil, "Credential process argv, repeat once per argument (no shell)")
		}
		r.AddCommand(c)
	}
	return r
}

// UIS numeric groups are documented under Data API / error handling. Unknown
// write errors remain ambiguous; recognized rejections retain their exit class.
func rpcErrorCode(e *client.RPCError, read bool) int {
	switch fmt.Sprint(e.Code) {
	case "-32001":
		return 3
	case "-32003":
		return 4
	case "-32029":
		return 8
	case "-32601", "-32008", "-32099":
		return 7
	case "-32602", "-32600", "-32700":
		return 2
	case "-32004", "-32007", "-32009":
		return 6
	case "-32603":
		if read {
			return 6
		}
		return 9
	}
	if read {
		return 6
	}
	return 9
}

func profileConfigPath(g *globalFlags) string {
	if g.ConfigPath != "" {
		return g.ConfigPath
	}
	return profile.Path()
}

// Annotation commands accept a call-session ID; writes require the communication
// ID from that session's report. Never assume those namespaces are interchangeable.
func annotateCall(g *globalFlags, method string, params map[string]interface{}, sessionID, from, till string) error {
	delete(params, "communication_id")
	name, _, err := profileFor(g)
	if err != nil {
		return err
	}
	plan := map[string]interface{}{"account": name, "session_id": sessionID, "method": method, "params": redactParams(params), "preflight": "resolve communication_id with get.calls_report"}
	if g.DryRun {
		return emit(g, plan)
	}
	if err = confirm(g, plan); err != nil {
		return err
	}
	id, err := positive(sessionID)
	if err != nil {
		return err
	}
	raw, err := rpc(g, "get.calls_report", map[string]interface{}{"date_from": from, "date_till": till, "limit": 1, "fields": []string{"id", "communication_id"}, "filter": map[string]interface{}{"field": "id", "operator": "=", "value": id}})
	if err != nil {
		return err
	}
	var data interface{}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&data) != nil {
		return exitf(7, "Invalid call identity response")
	}
	rows, err := portal.Rows(data)
	if err != nil {
		return exitf(7, "Invalid call identity response")
	}
	var communication json.Number
	for _, row := range rows {
		if portal.ID(row) != sessionID {
			continue
		}
		if communication != "" {
			return exitf(7, "Ambiguous call identity")
		}
		value := portal.ID(portal.Object{"id": row["communication_id"]})
		communication, err = positive(value)
		if err != nil {
			return exitf(7, "Call has no verified communication_id")
		}
	}
	if communication == "" {
		return exitf(5, "Call not found in date window")
	}
	params["communication_id"] = communication
	confirmed := *g
	confirmed.Force = true
	return runRPC(&confirmed, method, params)
}
