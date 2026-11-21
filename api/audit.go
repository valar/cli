package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ErrNoAuditAPI is returned when the server predates the audit log route.
var ErrNoAuditAPI = errors.New("the server has no audit log API; it needs a newer Valar platform")

// MaxAuditPage is the most rows the server returns in one page.
const MaxAuditPage = 500

// Audit row kinds: an attempt is written before a change and a result after
// it; a count sums a minute's refused requests.
const (
	AuditAttempt = "attempt"
	AuditResult  = "result"
	AuditCount   = "count"
)

// AuditEvent is a row of a project's audit log.
type AuditEvent struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	AttemptID int64     `json:"attemptId,omitempty"`
	Principal string    `json:"principal"`
	Owner     string    `json:"owner,omitempty"`
	TokenID   string    `json:"tokenId,omitempty"`
	Client    string    `json:"client,omitempty"`
	SourceIP  string    `json:"sourceIp,omitempty"`
	Service   string    `json:"service,omitempty"`
	Operation string    `json:"operation"`
	Tool      string    `json:"tool,omitempty"`
	Target    string    `json:"target,omitempty"`
	Status    int       `json:"status,omitempty"`
	Count     int       `json:"count"`
}

// ListAuditEvents returns a page of the project's audit log, newest first,
// with IDs below before if it is positive. A limit of 0 asks for the server's
// default page size.
func (client *Client) ListAuditEvents(project string, before int64, limit int) ([]AuditEvent, error) {
	params := url.Values{}
	if before > 0 {
		params.Set("before", strconv.FormatInt(before, 10))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	path := "/projects/" + project + "/audit"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var events []AuditEvent
	if err := client.request(http.MethodGet, path, &events, nil); err != nil {
		if unknownRoute(err) {
			return nil, ErrNoAuditAPI
		}
		return nil, err
	}
	return events, nil
}
