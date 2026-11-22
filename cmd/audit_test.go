package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/valar/cli/api"
)

// Row 1200's token ID is in a format the CLI does not know, so it is shown whole.
const auditPage = `[{"id":1201,"at":"2026-11-21T10:00:01Z","kind":"result","attemptId":1200,"principal":"human::alice","tokenId":"` + ciTokenID +
	`","client":"valar-cli/4.9.0","sourceIp":"192.0.2.1","service":"web","operation":"deploys.submit","target":"deployment=7","status":200,"count":0},` +
	`{"id":1200,"at":"2026-11-21T10:00:00Z","kind":"attempt","principal":"human::alice","tokenId":"future-id","operation":"tokens.create","count":0},` +
	`{"id":1190,"at":"2026-11-21T09:59:00Z","kind":"count","principal":"service::acme/web","operation":"env.patch","status":403,"count":3}]`

func TestListAudit(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Limit    int
		WantHint string
	}{
		{"FullPage", 3, "Older rows: valar audit --project acme --before 1190 --limit 3\n"},
		{"LastPage", 100, ""},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			client, _ := newFakeAPI(t, map[string]string{"GET /projects/acme/audit": auditPage})
			var out, progress bytes.Buffer
			if err := listAudit(client, "acme", 0, tc.Limit, &out, &progress); err != nil {
				t.Fatalf("listAudit: %v", err)
			}
			got := lines(out.String())
			want := []string{
				"ID TIME KIND PRINCIPAL TOKEN OPERATION SERVICE TARGET STATUS SOURCE CLIENT",
				"1201 <time> result human::alice fedcba98 deploys.submit web deployment=7 200 192.0.2.1 valar-cli/4.9.0",
				"1200 <time> attempt human::alice future-id tokens.create - - - - -",
				"1190 <time> count (3) service::acme/web - env.patch - - 403 - -",
			}
			if len(got) != len(want) {
				t.Fatalf("table\n got %q\nwant %q", got, want)
			}
			for i := 1; i < len(got); i++ {
				// The time is local; replace its date and clock fields.
				fields := strings.Fields(got[i])
				got[i] = strings.Join(append([]string{fields[0], "<time>"}, fields[3:]...), " ")
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("table\n got %q\nwant %q", got, want)
			}
			if progress.String() != tc.WantHint {
				t.Errorf("progress %q, want %q", progress.String(), tc.WantHint)
			}
		})
	}
}

func TestListAudit_JSON(t *testing.T) {
	out := setJSON(t, true)
	client, _ := newFakeAPI(t, map[string]string{"GET /projects/acme/audit": auditPage})
	var progress bytes.Buffer
	if err := listAudit(client, "acme", 0, 3, out, &progress); err != nil {
		t.Fatal(err)
	}
	var events []api.AuditEvent
	if err := json.Unmarshal(out.Bytes(), &events); err != nil || len(events) != 3 || events[2].Count != 3 {
		t.Errorf("stdout %s decodes to %+v, %v", out.String(), events, err)
	}
	if progress.Len() != 0 {
		t.Errorf("progress %q, want none in JSON mode", progress.String())
	}
}

func TestListAudit_Invalid(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Project string
		Before  int64
		Limit   int
		WantErr string
	}{
		{"NoProject", "", 0, 100, "no project"},
		{"BadProject", "acme/services", 0, 100, "invalid project name"},
		{"ZeroLimit", "acme", 0, 0, "--limit must be between 1 and 500"},
		{"LimitAboveMax", "acme", 0, 501, "--limit must be between 1 and 500"},
		{"NegativeBefore", "acme", -1, 100, "--before must be an event ID"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, requests := newFakeAPI(t, nil)
			err := listAudit(client, tc.Project, tc.Before, tc.Limit, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.WantErr) || classify(err, kindFailed) != kindInvalid {
				t.Errorf("error %v, want an invalid one containing %q", err, tc.WantErr)
			}
			if len(*requests) != 0 {
				t.Errorf("sent %v", *requests)
			}
		})
	}
}

func TestListAudit_OlderServer(t *testing.T) {
	client, _ := newFakeAPI(t, nil)
	err := listAudit(client, "acme", 0, 100, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, api.ErrNoAuditAPI) || !strings.Contains(err.Error(), "newer Valar platform") {
		t.Errorf("error = %v, want ErrNoAuditAPI", err)
	}
}
