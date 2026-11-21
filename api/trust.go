package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// GitHubIssuer issues the identity tokens of GitHub Actions jobs.
const GitHubIssuer = "https://token.actions.githubusercontent.com"

// ErrNoTrustAPI is returned when the server predates deploys from CI.
var ErrNoTrustAPI = errors.New("the server has no trust policies or token exchange; it needs a newer Valar platform")

// TrustPolicy lets CI jobs whose identity tokens come from Issuer, name
// Subject and carry Claims exchange them for tokens for Service that act as
// CreatedBy. Service is a name, or a preview prefix followed by *.
type TrustPolicy struct {
	ID        string            `json:"id"`
	Service   string            `json:"service"`
	Issuer    string            `json:"issuer"`
	Subject   string            `json:"subject"`
	Claims    map[string]string `json:"claims"`
	CreatedBy string            `json:"createdBy"`
	CreatedAt time.Time         `json:"createdAt"`
	// Warning is set when the policy was just created.
	Warning string `json:"warning,omitempty"`
}

// TrustPolicyRequest creates a trust policy. An empty Issuer is the
// installation's first.
type TrustPolicyRequest struct {
	Service string            `json:"service"`
	Subject string            `json:"subject"`
	Issuer  string            `json:"issuer,omitempty"`
	Claims  map[string]string `json:"claims,omitempty"`
}

// ExchangedToken is a token for one service, minted for a CI job's identity token.
type ExchangedToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func trustError(err error) error {
	if unknownRoute(err) {
		return ErrNoTrustAPI
	}
	return err
}

func trustPoliciesPath(project string) string {
	return fmt.Sprintf("/projects/%s/trust-policies", project)
}

// CreateTrustPolicy adds a trust policy to project. The calling token must
// have been created in the console or with authctl.
func (client *Client) CreateTrustPolicy(project string, req TrustPolicyRequest) (*TrustPolicy, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var policy TrustPolicy
	if err := client.request(http.MethodPost, trustPoliciesPath(project), &policy, bytes.NewReader(payload)); err != nil {
		return nil, trustError(err)
	}
	return &policy, nil
}

// ListTrustPolicies returns the project's trust policies, oldest first.
func (client *Client) ListTrustPolicies(project string) ([]TrustPolicy, error) {
	var policies []TrustPolicy
	if err := client.request(http.MethodGet, trustPoliciesPath(project), &policies, nil); err != nil {
		return nil, trustError(err)
	}
	return policies, nil
}

// DeleteTrustPolicy deletes a trust policy and returns how many of its tokens
// it revoked.
func (client *Client) DeleteTrustPolicy(project, id string) (int, error) {
	var resp struct {
		Revoked int `json:"revoked"`
	}
	if err := client.request(http.MethodDelete, trustPoliciesPath(project)+"/"+id, &resp, nil); err != nil {
		return 0, trustError(err)
	}
	return resp.Revoked, nil
}

// ExchangeToken trades a CI job's identity token for a token for one service.
// The request carries no bearer token, so the client needs none.
func (client *Client) ExchangeToken(project, service, idToken string) (*ExchangedToken, error) {
	payload, err := json.Marshal(struct {
		IDToken string `json:"idToken"`
	}{idToken})
	if err != nil {
		return nil, err
	}
	anonymous := *client
	anonymous.Token = ""
	var token ExchangedToken
	path := fmt.Sprintf("/projects/%s/services/%s/token-exchange", project, service)
	if err := anonymous.request(http.MethodPost, path, &token, bytes.NewReader(payload)); err != nil {
		return nil, trustError(err)
	}
	return &token, nil
}

// InspectedService is a service's listing entry with its URLs: those of the
// linked custom domains, then those of the canonical domains.
type InspectedService struct {
	Service
	URLs []string `json:"urls"`
}

// ErrNoInspectAPI is returned when the server predates the inspect route.
var ErrNoInspectAPI = errors.New("the server has no service inspect route")

// InspectService returns one service with its URLs. Unlike the listing, a
// token bound to the service may read it.
func (client *Client) InspectService(project, service string) (*InspectedService, error) {
	var svc InspectedService
	if err := client.request(http.MethodGet, fmt.Sprintf("/projects/%s/services/%s/inspect", project, service), &svc, nil); err != nil {
		if unknownRoute(err) {
			return nil, ErrNoInspectAPI
		}
		return nil, err
	}
	return &svc, nil
}
