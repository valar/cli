package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrNoSettingsAPI is returned when the server predates service settings.
var ErrNoSettingsAPI = errors.New("the server has no service settings API; it needs a newer Valar platform")

// Setting names, as the API and .valar.yml spell them.
const (
	SettingCPU        = "cpu"
	SettingMemory     = "memory"
	SettingHTTP       = "http"
	SettingMinRunning = "minRunning"
)

// ServiceSettings is a full set of settings with defaults filled in. CPU is in
// millicores, Memory in MiB.
type ServiceSettings struct {
	CPU        int  `json:"cpu"`
	Memory     int  `json:"memory"`
	HTTP       bool `json:"http"`
	MinRunning int  `json:"minRunning"`
}

// SettingsPatch maps setting names to new values; a nil value resets the
// setting to its default. Names left out keep their value.
type SettingsPatch map[string]any

type SettingsBound struct {
	Min  int `json:"min"`
	Max  int `json:"max"`
	Step int `json:"step,omitempty"`
}

type Allowance struct {
	Used    int `json:"used"`
	Allowed int `json:"allowed"`
}

type SettingsLimits struct {
	CPU        SettingsBound `json:"cpu"`
	Memory     SettingsBound `json:"memory"`
	MinRunning SettingsBound `json:"minRunning"`
	// Budget and RunningInstances are nil on a single-user installation.
	Budget *struct {
		Memory Allowance `json:"memory"`
		CPU    Allowance `json:"cpu"`
	} `json:"budget,omitempty"`
	RunningInstances *Allowance `json:"runningInstances,omitempty"`
}

// SettingsView is a service's settings, the ones that were set explicitly,
// and their limits. Raw is the response as the server sent it.
type SettingsView struct {
	Settings ServiceSettings `json:"settings"`
	Explicit []string        `json:"explicit"`
	Limits   SettingsLimits  `json:"limits"`
	Raw      json.RawMessage `json:"-"`
}

func settingsPath(project, service string) string {
	return fmt.Sprintf("/projects/%s/services/%s/settings", project, service)
}

// settingsError maps a route the server does not know to ErrNoSettingsAPI.
func settingsError(err error) error {
	if unknownRoute(err) {
		return ErrNoSettingsAPI
	}
	return err
}

func (client *Client) settingsRequest(method, project, service string, body io.Reader) (*SettingsView, error) {
	var raw json.RawMessage
	if err := client.request(method, settingsPath(project, service), &raw, body); err != nil {
		return nil, settingsError(err)
	}
	view := SettingsView{Raw: raw}
	if err := json.Unmarshal(raw, &view); err != nil {
		return nil, fmt.Errorf("unmarshalling settings: %w", err)
	}
	return &view, nil
}

// ServiceSettings returns the service's current settings and their limits.
func (client *Client) ServiceSettings(project, service string) (*SettingsView, error) {
	return client.settingsRequest(http.MethodGet, project, service, nil)
}

// PatchSettings merges patch into the service's settings, which the next
// deployment pins.
func (client *Client) PatchSettings(project, service string, patch SettingsPatch) (*SettingsView, error) {
	payload, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return client.settingsRequest(http.MethodPatch, project, service, bytes.NewReader(payload))
}
