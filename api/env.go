package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// EnvMinPlatform is the first platform release that serves the environment API.
const EnvMinPlatform = "2.9.0"

// ErrNoEnvAPI is returned when the server predates the environment API.
var ErrNoEnvAPI = fmt.Errorf("the server has no environment API, which needs Valar platform %s or newer", EnvMinPlatform)

// Environment targets: runtime variables reach deployments and batch
// executions, build variables reach builds.
const (
	EnvTargetRuntime = "runtime"
	EnvTargetBuild   = "build"
)

// EnvVariable is a stored variable. Value is empty for secrets, which the
// server never returns.
type EnvVariable struct {
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	Secret    bool      `json:"secret"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
}

type EnvValue struct {
	Value   string   `json:"value"`
	Secret  bool     `json:"secret"`
	Targets []string `json:"targets"`
}

// EnvChangeset is applied atomically. Unset maps a name to its targets.
// OnlySeeded applies an entry only where the variable is unchanged since the
// server seeded it from the legacy lists.
type EnvChangeset struct {
	Set        map[string]EnvValue `json:"set,omitempty"`
	Unset      map[string][]string `json:"unset,omitempty"`
	OnlySeeded bool                `json:"onlySeeded,omitempty"`
}

type EnvChangesetResult struct {
	// Skipped names the entries OnlySeeded left alone.
	Skipped []string `json:"skipped"`
}

// EnvChange is an entry of the change log. It never carries a value.
type EnvChange struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	Action    string    `json:"action"`
	Origin    string    `json:"origin"`
	Secret    bool      `json:"secret"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

func envPath(project, service string) string {
	return fmt.Sprintf("/projects/%s/services/%s/env", project, service)
}

// envError maps a route the server does not know to ErrNoEnvAPI. The router
// answers those in plain text, while API errors are JSON.
func envError(err error) error {
	var apiErr Error
	if !errors.As(err, &apiErr) {
		return err
	}
	var jsonErr JSONError
	missing := apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusMethodNotAllowed
	if missing && !errors.As(apiErr.ServerError, &jsonErr) {
		return ErrNoEnvAPI
	}
	return err
}

// ListEnv returns the service's current sets.
func (client *Client) ListEnv(project, service string) ([]EnvVariable, error) {
	var vars []EnvVariable
	if err := client.request(http.MethodGet, envPath(project, service), &vars, nil); err != nil {
		return nil, envError(err)
	}
	return vars, nil
}

// PatchEnv applies a changeset to the service's current sets.
func (client *Client) PatchEnv(project, service string, changes *EnvChangeset) (*EnvChangesetResult, error) {
	payload, err := json.Marshal(changes)
	if err != nil {
		return nil, err
	}
	var result EnvChangesetResult
	if err := client.request(http.MethodPatch, envPath(project, service), &result, bytes.NewReader(payload)); err != nil {
		return nil, envError(err)
	}
	return &result, nil
}

// ListEnvChanges returns the service's change log, newest first.
func (client *Client) ListEnvChanges(project, service string) ([]EnvChange, error) {
	var changes []EnvChange
	if err := client.request(http.MethodGet, envPath(project, service)+"/changes", &changes, nil); err != nil {
		return nil, envError(err)
	}
	return changes, nil
}

// HasEnvAPI reports whether the server keeps the service's environment, so
// requests must not carry inline lists. It is false for servers without the
// environment API and for servers that have it disabled (503), both of which
// still take the lists from .valar.yml. Any other API error, such as an
// unknown service or a missing read permission, comes from a server that has
// the API.
func (client *Client) HasEnvAPI(project, service string) (bool, error) {
	_, err := client.ListEnv(project, service)
	var apiErr Error
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoEnvAPI):
		return false, nil
	case errors.As(err, &apiErr):
		return apiErr.StatusCode != http.StatusServiceUnavailable, nil
	default:
		return false, err
	}
}
