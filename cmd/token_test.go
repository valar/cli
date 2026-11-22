package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/valar/cli/api"
)

const (
	valarHint = "Create a new token at https://console.valar.dev/api-tokens/new and use it with 'valar config endpoint set <endpoint> --token <token>' or VALAR_TOKEN."
	otherHint = "Create a new token in the Valar console and use it with 'valar config endpoint set <endpoint> --token <token>' or VALAR_TOKEN."
	valarAPI  = "https://api.valar.dev/v2"
	hostedAPI = "https://valar.example.com/v2"
	day       = 24 * time.Hour
)

func TestExpiryNotice(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		Name     string
		Endpoint string
		Expires  []time.Time
		Want     string
	}{
		{"WithinWindow", valarAPI, []time.Time{now.Add(6 * day)}, "Warning: your Valar token expires in 6 days, at 2026-11-27 10:00 UTC. " + valarHint + "\n"},
		{"OncePerInvocation", valarAPI, []time.Time{now.Add(3 * time.Hour), now.Add(3 * time.Hour), now.Add(time.Hour)}, "Warning: your Valar token expires in 3 hours, at 2026-11-21 13:00 UTC. " + valarHint + "\n"},
		{"BeyondWindow", valarAPI, []time.Time{now.Add(15 * day)}, ""},
		{"BeyondThenWithin", valarAPI, []time.Time{now.Add(15 * day), now.Add(14 * day)}, "Warning: your Valar token expires in 14 days, at 2026-12-05 10:00 UTC. " + valarHint + "\n"},
		{"OtherEndpoint", hostedAPI, []time.Time{now.Add(13 * day)}, "Warning: your Valar token expires in 13 days, at 2026-12-04 10:00 UTC. " + otherHint + "\n"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var out bytes.Buffer
			notice := &expiryNotice{w: &out, now: func() time.Time { return now }}
			for _, expires := range tc.Expires {
				notice.observe(tc.Endpoint, expires)
			}
			if out.String() != tc.Want {
				t.Errorf("got %q, want %q", out.String(), tc.Want)
			}
		})
	}
}

func TestUntilExpiry(t *testing.T) {
	for _, tc := range []struct {
		Left time.Duration
		Want string
	}{
		{5*day - time.Second, "in 5 days"},
		{2 * day, "in 2 days"},
		{47 * time.Hour, "in 47 hours"},
		{2 * time.Hour, "in 2 hours"},
		{90 * time.Minute, "in 90 minutes"},
		{time.Minute, "in 1 minute"},
		{10 * time.Second, "within a minute"},
		{-time.Minute, "within a minute"},
	} {
		t.Run(tc.Want, func(t *testing.T) {
			if got := untilExpiry(tc.Left); got != tc.Want {
				t.Errorf("untilExpiry(%s) = %q, want %q", tc.Left, got, tc.Want)
			}
		})
	}
}

func TestExpiryNotice_OnceAcrossRequests(t *testing.T) {
	const token = "valar_ut_0123456789abcdef"
	expires := time.Now().Add(3 * day).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(api.TokenExpiresHeader, expires.Format(time.RFC3339))
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":"v2"}`))
			return
		}
		w.Write([]byte(`{"name":"ada"}`))
	}))
	t.Cleanup(srv.Close)
	var out bytes.Buffer
	prev := api.OnTokenExpiry
	api.OnTokenExpiry = (&expiryNotice{w: &out, now: time.Now}).observe
	t.Cleanup(func() { api.OnTokenExpiry = prev })

	client, err := api.NewClient(srv.URL, token)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	for range 3 {
		if _, err := client.UserInfo(); err != nil {
			t.Fatalf("UserInfo: %v", err)
		}
	}
	if n := strings.Count(out.String(), "Warning:"); n != 1 {
		t.Errorf("warned %d times, want once: %q", n, out.String())
	}
	if !strings.Contains(out.String(), expires.Format("2006-01-02 15:04 MST")) {
		t.Errorf("warning %q lacks the expiry", out.String())
	}
	if strings.Contains(out.String(), token) {
		t.Errorf("warning %q prints the token", out.String())
	}
}

func TestReportError_TokenRefused(t *testing.T) {
	const reason = "token 3f2a... expired at 2027-02-18T10:00:00Z"
	refused := fmt.Errorf("listing services: %w", api.Error{
		StatusCode:  http.StatusUnauthorized,
		ServerError: api.TokenRefusedError{Reason: reason, Endpoint: valarAPI},
	})
	for _, tc := range []struct {
		Name string
		JSON bool
		Want string
	}{
		{"Human", false, "listing services: Unauthorized: " + reason + "\n" + valarHint + "\n"},
		{"JSON", true, `{"error":{"kind":"notAllowed","message":"listing services: Unauthorized: ` + reason + `\n` + valarHint + `"}}` + "\n"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, tc.JSON)
			var stderr bytes.Buffer
			if code := reportError(&stderr, refused, kindFailed, false); code != int(kindNotAllowed) {
				t.Errorf("code %d, want %d", code, kindNotAllowed)
			}
			if stderr.String() != tc.Want {
				t.Errorf("stderr %q, want %q", stderr.String(), tc.Want)
			}
		})
	}
}

const (
	rootTokenID = "0123456789abcdef0123456789abcdef"
	ciTokenID   = "fedcba9876543210fedcba9876543210"
	ciMinted    = `{"id":"` + ciTokenID + `","kind":"user","name":"ci","project":"acme","scopes":["deploy","read"],"parent":"` + rootTokenID +
		`","createdAt":"2026-11-21T12:00:00Z","expiresAt":"2027-02-19T12:00:00Z","value":"valar_ut_secret"}`
)

var tokenNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// lines splits out into lines with runs of spaces collapsed, so that table
// and row checks don't depend on column widths.
func lines(out string) []string {
	var ls []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		ls = append(ls, strings.Join(strings.Fields(l), " "))
	}
	return ls
}

func containsAll(t *testing.T, what, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s %q lacks %q", what, got, w)
		}
	}
}

func TestParseTTL(t *testing.T) {
	for _, tc := range []struct {
		In      string
		Want    time.Duration
		WantErr bool
	}{
		{"90d", 90 * day, false},
		{"1d", day, false},
		{"12h", 12 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"0d", 0, true},
		{"-1d", 0, true},
		{"d", 0, true},
		{"90", 0, true},
		{"2w", 0, true},
		{"-5m", 0, true},
		{"999999999999d", 0, true},
	} {
		t.Run(tc.In, func(t *testing.T) {
			got, err := parseTTL(tc.In)
			if (err != nil) != tc.WantErr || got != tc.Want {
				t.Errorf("parseTTL(%q) = %s, %v; want %s, error %v", tc.In, got, err, tc.Want, tc.WantErr)
			}
		})
	}
}

func TestNewTokenRequest(t *testing.T) {
	for _, tc := range []struct {
		Name             string
		Project, Service string
		Scopes           []string
		TTL              string
		Agent            bool
		Want             api.TokenRequest
		WantKind         errorKind
	}{
		{Name: "Defaults", Want: api.TokenRequest{Name: "ci"}},
		{
			Name: "DaysInHours", Project: "acme", Scopes: []string{"read", "deploy"}, TTL: "90d",
			Want: api.TokenRequest{Name: "ci", Project: "acme", Scopes: []string{"read", "deploy"}, TTL: "2160h"},
		},
		{Name: "Duration", TTL: "90m", Want: api.TokenRequest{Name: "ci", TTL: "1h30m0s"}},
		{Name: "Service", Project: "acme", Service: "web", Want: api.TokenRequest{Name: "ci", Project: "acme", Service: "web"}},
		{Name: "ServiceWithoutProject", Service: "web", WantKind: kindInvalid},
		{Name: "BadTTL", TTL: "soon", WantKind: kindInvalid},
		{Name: "Agent", Project: "acme", Agent: true, Want: api.TokenRequest{Name: "ci", Kind: "agent", Project: "acme", Scopes: []string{"read"}}},
		{
			Name: "AgentForServiceAtMost", Project: "acme", Service: "web", Scopes: []string{"read"}, TTL: "30d", Agent: true,
			Want: api.TokenRequest{Name: "ci", Kind: "agent", Project: "acme", Service: "web", Scopes: []string{"read"}, TTL: "720h"},
		},
		{Name: "AgentWithoutProject", Agent: true, WantKind: kindInvalid},
		{Name: "AgentThatDeploys", Project: "acme", Scopes: []string{"read", "deploy"}, Agent: true, WantKind: kindInvalid},
		{Name: "AgentBeyond30Days", Project: "acme", TTL: "31d", Agent: true, WantKind: kindInvalid},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			req, _, err := newTokenRequest("ci", tc.Project, tc.Service, tc.Scopes, tc.TTL, tc.Agent)
			if tc.WantKind != 0 {
				if got := classify(err, kindFailed); err == nil || got != tc.WantKind {
					t.Errorf("error %v of kind %v, want kind %v", err, got, tc.WantKind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(req, tc.Want) {
				t.Errorf("request %+v, want %+v", req, tc.Want)
			}
		})
	}
}

func TestCreateToken(t *testing.T) {
	clamped := strings.Replace(ciMinted, "2027-02-19T12:00:00Z", "2026-11-22T12:00:00Z", 1)
	for _, tc := range []struct {
		Name         string
		JSON         bool
		TTL          time.Duration
		Response     string
		WantProgress []string
		NotProgress  []string
	}{
		{
			Name: "Human", TTL: 90 * day, Response: ciMinted,
			WantProgress: []string{
				"Created token ci.", "ID: " + ciTokenID, "Binding: project acme", "Scopes: deploy, read",
				"Expires: 2027-02-19 12:00 UTC", "Parent: " + rootTokenID, "Valar shows it only once",
			},
			NotProgress: []string{"valar_ut_secret", "expires with the token that created it"},
		},
		{
			Name: "Clamped", TTL: 90 * day, Response: clamped,
			WantProgress: []string{"Expires: 2026-11-22 12:00 UTC", "expires with the token that created it"},
		},
		{Name: "ServerDefaultTTL", Response: clamped, NotProgress: []string{"expires with the token that created it"}},
		{Name: "JSON", JSON: true, Response: ciMinted},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, tc.JSON)
			client, requests := newFakeAPI(t, map[string]string{"POST /tokens": tc.Response})
			var out, progress bytes.Buffer
			req := api.TokenRequest{Name: "ci", Project: "acme", Scopes: []string{"read", "deploy"}, TTL: "2160h"}
			if err := createToken(client, req, tc.TTL, &out, &progress, tokenNow); err != nil {
				t.Fatalf("createToken: %v", err)
			}
			if got, want := (*requests)[0].Body, `{"name":"ci","project":"acme","scopes":["read","deploy"],"ttl":"2160h"}`; got != want {
				t.Errorf("body = %s, want %s", got, want)
			}
			if tc.JSON {
				var minted api.MintedToken
				if err := json.Unmarshal(out.Bytes(), &minted); err != nil || minted.Value != "valar_ut_secret" || minted.ID != ciTokenID {
					t.Errorf("stdout %s decodes to %+v, %v", out.String(), minted, err)
				}
				if progress.Len() != 0 {
					t.Errorf("progress %q, want none in JSON mode", progress.String())
				}
				return
			}
			if out.String() != "valar_ut_secret\n" {
				t.Errorf("stdout %q, want only the token", out.String())
			}
			got := strings.Join(lines(progress.String()), "\n")
			containsAll(t, "progress", got, tc.WantProgress...)
			for _, unwanted := range tc.NotProgress {
				if strings.Contains(got, unwanted) {
					t.Errorf("progress %q contains %q", got, unwanted)
				}
			}
		})
	}
}

func TestCreateToken_Agent(t *testing.T) {
	const minted = `{"id":"` + ciTokenID + `","kind":"agent","name":"claude","project":"acme","scopes":["read"],"parent":"` + rootTokenID +
		`","createdAt":"2026-11-21T12:00:00Z","expiresAt":"2026-11-28T12:00:00Z","value":"valar_at_secret"}`
	req := api.TokenRequest{Name: "claude", Kind: "agent", Project: "acme", Scopes: []string{"read"}}
	t.Run("Human", func(t *testing.T) {
		setJSON(t, false)
		client, requests := newFakeAPI(t, map[string]string{"POST /tokens": minted})
		var out, progress bytes.Buffer
		if err := createToken(client, req, 0, &out, &progress, tokenNow); err != nil {
			t.Fatalf("createToken: %v", err)
		}
		if got, want := (*requests)[0].Body, `{"name":"claude","kind":"agent","project":"acme","scopes":["read"]}`; got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
		if out.String() != "valar_at_secret\n" {
			t.Errorf("stdout %q, want only the token", out.String())
		}
		mcp := client.Endpoint + "/mcp"
		containsAll(t, "progress", progress.String(),
			"Kind:", "agent", "Binding:", "project acme",
			"claude mcp add --transport http valar "+mcp,
			"--header 'Authorization: Bearer ${VALAR_AGENT_TOKEN}'",
			`"url": "`+mcp+`"`,
			"~/.valar",
		)
		if strings.Contains(progress.String(), "valar_at_secret") {
			t.Errorf("progress %q shows the token", progress.String())
		}
	})
	t.Run("JSON", func(t *testing.T) {
		setJSON(t, true)
		client, _ := newFakeAPI(t, map[string]string{"POST /tokens": minted})
		var out, progress bytes.Buffer
		if err := createToken(client, req, 0, &out, &progress, tokenNow); err != nil {
			t.Fatalf("createToken: %v", err)
		}
		var doc struct {
			api.MintedToken
			MCPURL string `json:"mcpUrl"`
		}
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Value != "valar_at_secret" || doc.Kind != "agent" || doc.MCPURL != client.Endpoint+"/mcp" {
			t.Errorf("stdout %s decodes to %+v, %v", out.String(), doc, err)
		}
	})
}

func TestMCPURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.valar.dev/v2":         "https://api.valar.dev/mcp",
		"https://api.valar.dev/v2/":        "https://api.valar.dev/mcp",
		"https://valar.example.com/api/v2": "https://valar.example.com/api/mcp",
		"http://localhost:8080":            "http://localhost:8080/mcp",
	} {
		if got := mcpURL(in); got != want {
			t.Errorf("mcpURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateToken_Refused(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Status   int
		Routes   map[string]string
		WantKind errorKind
		WantErr  string
	}{
		{"OlderServer", 0, nil, kindFailed, "needs a newer Valar platform"},
		{
			"Child", http.StatusForbidden,
			map[string]string{"POST /tokens": `{"error":"only tokens minted in the console or with authctl can mint tokens"}`},
			kindNotAllowed, "Forbidden: only tokens minted in the console or with authctl can mint tokens",
		},
		{"Taken", http.StatusConflict, map[string]string{"POST /tokens": `{"error":"a live token with that name exists"}`}, kindConflict, "a live token with that name exists"},
		{"BadBinding", http.StatusBadRequest, map[string]string{"POST /tokens": `{"error":"service is not a service name"}`}, kindInvalid, "service is not a service name"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			client, _ := newFakeAPIWithStatus(t, tc.Routes, map[string]int{"POST /tokens": tc.Status})
			var out, progress bytes.Buffer
			err := createToken(client, api.TokenRequest{Name: "ci"}, 0, &out, &progress, tokenNow)
			if err == nil || !strings.Contains(err.Error(), tc.WantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tc.WantErr)
			}
			if got := classify(err, kindFailed); got != tc.WantKind {
				t.Errorf("kind = %v, want %v", got, tc.WantKind)
			}
			if out.Len() != 0 || progress.Len() != 0 {
				t.Errorf("printed %q and %q on a refusal", out.String(), progress.String())
			}
		})
	}
}

func TestListTokens(t *testing.T) {
	const (
		oldID     = "11111111111111111111111111111111"
		expiredID = "22222222222222222222222222222222"
		own       = `[{"id":"` + rootTokenID + `","kind":"user","name":"laptop","scopes":["all"],"createdAt":"2026-11-01T12:00:00Z","expiresAt":"2027-01-30T12:00:00Z","lastUsed":"2026-11-21T11:59:00Z"},` +
			`{"id":"` + ciTokenID + `","kind":"user","name":"ci","project":"acme","scopes":["deploy","read"],"parent":"` + rootTokenID + `","expiresAt":"2027-01-30T12:00:00Z"},` +
			`{"id":"` + oldID + `","kind":"user","name":"old","service":"web","project":"acme","scopes":["read"],"expiresAt":"2027-01-30T12:00:00Z","revokedAt":"2026-11-20T12:00:00Z"},` +
			`{"id":"` + expiredID + `","kind":"future-kind","name":"cli","scopes":["all"],"expiresAt":"2026-11-21T11:00:00Z"}]`
		project = `[{"id":"` + ciTokenID + `","kind":"user","name":"ci","owner":"alice","project":"acme","scopes":["deploy","read"],"parent":"` + rootTokenID + `"}]`
	)
	routes := map[string]string{"GET /tokens": own, "GET /projects/acme/tokens": project}
	for _, tc := range []struct {
		Name      string
		Project   string
		WantRoute string
		WantRows  []string
	}{
		{
			"Own", "", "/tokens", []string{
				"ID NAME KIND BINDING SCOPES PARENT EXPIRES LAST USED STATUS",
				rootTokenID + " laptop user - all - 2 months from now 1 minute ago live",
				ciTokenID + " ci user acme deploy,read laptop 2 months from now never live",
				oldID + " old user acme/web read - 2 months from now never revoked",
				expiredID + " cli future-kind - all - 1 hour ago never expired",
			},
		},
		{
			"Project", "acme", "/projects/acme/tokens", []string{
				"ID NAME OWNER KIND BINDING SCOPES PARENT EXPIRES LAST USED STATUS",
				ciTokenID + " ci alice user acme deploy,read " + rootTokenID + " never never live",
			},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			client, requests := newFakeAPI(t, routes)
			var out bytes.Buffer
			if err := listTokens(client, tc.Project, &out, tokenNow); err != nil {
				t.Fatalf("listTokens: %v", err)
			}
			if got := (*requests)[0].Path; got != tc.WantRoute {
				t.Errorf("path = %s, want %s", got, tc.WantRoute)
			}
			if got := lines(out.String()); !reflect.DeepEqual(got, tc.WantRows) {
				t.Errorf("table\n got %q\nwant %q", got, tc.WantRows)
			}
		})
	}
}

func TestListTokens_JSON(t *testing.T) {
	out := setJSON(t, true)
	client, _ := newFakeAPI(t, map[string]string{"GET /projects/acme/tokens": `[{"id":"` + ciTokenID + `","kind":"user","name":"ci","owner":"alice","scopes":["read"]}]`})
	if err := listTokens(client, "acme", out, tokenNow); err != nil {
		t.Fatal(err)
	}
	var tokens []map[string]any
	if err := json.Unmarshal(out.Bytes(), &tokens); err != nil || len(tokens) != 1 || tokens[0]["owner"] != "alice" {
		t.Errorf("stdout %s decodes to %v, %v", out.String(), tokens, err)
	}
	if strings.Contains(out.String(), "expiresAt") {
		t.Errorf("stdout %s shows an expiry the server left out", out.String())
	}
}

func TestRevokeToken(t *testing.T) {
	for _, tc := range []struct {
		Name      string
		Project   string
		ID        string
		Revoked   int
		WantRoute string
		Want      string
		WantKind  errorKind
	}{
		{Name: "One", ID: ciTokenID, Revoked: 1, WantRoute: "DELETE /tokens/" + ciTokenID, Want: "Revoked token " + ciTokenID + "."},
		{Name: "OneChild", ID: rootTokenID, Revoked: 2, WantRoute: "DELETE /tokens/" + rootTokenID, Want: "Revoked token " + rootTokenID + " and 1 token it created."},
		{Name: "Children", ID: rootTokenID, Revoked: 4, WantRoute: "DELETE /tokens/" + rootTokenID, Want: "Revoked token " + rootTokenID + " and 3 tokens it created."},
		{Name: "Already", ID: ciTokenID, Revoked: 0, WantRoute: "DELETE /tokens/" + ciTokenID, Want: "Token " + ciTokenID + " was already revoked."},
		{Name: "Project", Project: "acme", ID: ciTokenID, Revoked: 1, WantRoute: "DELETE /projects/acme/tokens/" + ciTokenID, Want: "Revoked token " + ciTokenID + "."},
		{Name: "Name", ID: "ci", WantKind: kindInvalid, Want: "is not a token ID"},
		{Name: "Uppercase", ID: strings.ToUpper(ciTokenID), WantKind: kindInvalid, Want: "is not a token ID"},
		{Name: "BadProject", Project: "acme/services", ID: ciTokenID, WantKind: kindInvalid, Want: "invalid project name"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			body := fmt.Sprintf(`{"revoked":%d}`, tc.Revoked)
			client, requests := newFakeAPI(t, map[string]string{"DELETE /tokens/" + tc.ID: body, "DELETE /projects/acme/tokens/" + tc.ID: body})
			var out bytes.Buffer
			err := revokeToken(client, tc.Project, tc.ID, &out)
			if tc.WantKind != 0 {
				if err == nil || !strings.Contains(err.Error(), tc.Want) || classify(err, kindFailed) != tc.WantKind {
					t.Errorf("error %v, want kind %v containing %q", err, tc.WantKind, tc.Want)
				}
				if len(*requests) != 0 {
					t.Errorf("sent %v for an invalid argument", *requests)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := routeOrder(*requests); len(got) != 1 || got[0] != tc.WantRoute {
				t.Errorf("requests %v, want [%s]", got, tc.WantRoute)
			}
			if got := strings.TrimSpace(out.String()); got != tc.Want {
				t.Errorf("printed %q, want %q", got, tc.Want)
			}
		})
	}
}

func TestRevokeToken_JSON(t *testing.T) {
	out := setJSON(t, true)
	client, _ := newFakeAPI(t, map[string]string{"DELETE /tokens/" + rootTokenID: `{"revoked":3}`})
	if err := revokeToken(client, "", rootTokenID, out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(strings.Fields(out.String()), ""), `{"id":"`+rootTokenID+`","revoked":3}`; got != want {
		t.Errorf("printed %s, want %s", got, want)
	}
}

func TestTokenCommands_OlderServer(t *testing.T) {
	setJSON(t, false)
	client, _ := newFakeAPI(t, nil)
	for name, err := range map[string]error{
		"list":   listTokens(client, "", io.Discard, tokenNow),
		"revoke": revokeToken(client, "", ciTokenID, io.Discard),
	} {
		if !errors.Is(err, api.ErrNoTokenAPI) || !strings.Contains(err.Error(), "newer Valar platform") {
			t.Errorf("%s: error = %v, want ErrNoTokenAPI", name, err)
		}
	}
}

func TestPrintUserInfo(t *testing.T) {
	for _, tc := range []struct {
		Name string
		Info api.UserInfo
		Want []string
	}{
		{
			"Grant",
			api.UserInfo{
				Name: "alice", Projects: []string{"acme", "demo"}, Principal: "human::alice",
				Token: &api.Token{
					ID: ciTokenID, Kind: "user", Name: "ci", Project: "acme", Service: "web", Scopes: []string{"deploy", "read"},
					Parent: rootTokenID, CreatedAt: tokenNow.Add(-2 * time.Hour), ExpiresAt: tokenNow.Add(30 * day), LastUsed: tokenNow.Add(-time.Minute),
				},
			},
			[]string{
				"User: alice",
				"Principal: human::alice",
				"Projects: acme, demo",
				"Token: ci",
				"ID: " + ciTokenID,
				"Kind: user",
				"Binding: service acme/web",
				"Scopes: deploy, read",
				"Created: 2026-11-21 10:00 UTC (2 hours ago)",
				"Expires: 2026-12-21 12:00 UTC (1 month from now)",
				"Last used: 2026-11-21 11:59 UTC (1 minute ago)",
				"Parent: " + rootTokenID,
			},
		},
		{
			// Servers before the token routes report neither creation, use nor parent.
			"EarlierGrant",
			api.UserInfo{
				Name: "alice", Projects: []string{"acme"}, Principal: "human::alice",
				Token: &api.Token{ID: rootTokenID, Kind: "user", Scopes: []string{"all"}},
			},
			[]string{
				"User: alice", "Principal: human::alice", "Projects: acme", "Token: -", "ID: " + rootTokenID,
				"Kind: user", "Binding: any project", "Scopes: all", "Expires: never",
			},
		},
		{"NoGrant", api.UserInfo{Name: "alice", Projects: []string{"acme"}}, []string{"User: alice", "Projects: acme"}},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var out bytes.Buffer
			if err := printUserInfo(&out, &tc.Info, tokenNow); err != nil {
				t.Fatal(err)
			}
			if got := lines(out.String()); !reflect.DeepEqual(got, tc.Want) {
				t.Errorf("printed\n got %q\nwant %q", got, tc.Want)
			}
		})
	}
}

// runTokenCLI runs the root command and resets the flags of the token and
// audit commands afterwards.
func runTokenCLI(t *testing.T, endpoint string, args ...string) string {
	t.Helper()
	t.Cleanup(func() {
		tokenProject, tokenService, tokenScopes, tokenTTL, tokenAgent = "", "", nil, "", false
		auditProject, auditLimit, auditBefore = "", defaultAuditLimit, 0
	})
	return runCLI(t, endpoint, args...)
}

func TestTokenCommands_Flags(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Args     []string
		Route    string
		Response string
		WantBody string
	}{
		{
			"Create",
			[]string{"token", "create", "ci", "--project", "acme", "--scopes", "read,deploy", "--ttl", "30d"},
			"POST /tokens", ciMinted, `{"name":"ci","project":"acme","scopes":["read","deploy"],"ttl":"720h"}`,
		},
		{
			"CreateAgent",
			[]string{"token", "create", "claude", "--agent", "--project", "acme", "--ttl", "30d"},
			"POST /tokens", ciMinted, `{"name":"claude","kind":"agent","project":"acme","scopes":["read"],"ttl":"720h"}`,
		},
		{"ListProject", []string{"token", "list", "--project", "acme"}, "GET /projects/acme/tokens", `[]`, ""},
		{"RevokeProject", []string{"token", "revoke", ciTokenID, "--project", "acme"}, "DELETE /projects/acme/tokens/" + ciTokenID, `{"revoked":1}`, ""},
		{"AuditActiveProject", []string{"audit"}, "GET /projects/p/audit", `[]`, ""},
		{"AuditProject", []string{"audit", "--project", "acme", "--limit", "5"}, "GET /projects/acme/audit", `[]`, ""},
		{"Whoami", []string{"whoami"}, "GET /users/info", `{"name":"alice","projects":[],"token":{"kind":"user","scopes":["all"]}}`, ""},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			writeServiceConfig(t, deployConfig)
			client, requests := newFakeAPI(t, map[string]string{tc.Route: tc.Response})
			out := runTokenCLI(t, client.Endpoint, tc.Args...)
			if got := routeOrder(*requests); len(got) != 1 || got[0] != tc.Route {
				t.Fatalf("requests %v, want [%s]", got, tc.Route)
			}
			if tc.WantBody != "" && (*requests)[0].Body != tc.WantBody {
				t.Errorf("body = %s, want %s", (*requests)[0].Body, tc.WantBody)
			}
			if !json.Valid([]byte(out)) {
				t.Errorf("output %q is not JSON", out)
			}
		})
	}
}
