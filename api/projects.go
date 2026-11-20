package api

import (
	"bytes"
	"encoding/json"
	"net/http"
)

type Project struct {
	Name    string `json:"name"`
	Creator string `json:"creator,omitempty"`
	Public  bool   `json:"public"`
}

// CreateProject creates a project owned by the calling user. Anyone can invoke
// the services of a public project without a token.
func (client *Client) CreateProject(name string, public bool) (*Project, error) {
	payload, err := json.Marshal(Project{Name: name, Public: public})
	if err != nil {
		return nil, err
	}
	var project Project
	if err := client.request(http.MethodPost, "/projects", &project, bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	return &project, nil
}

// DeleteProject deletes a project and retires its name for good. Without
// cascade the project must be empty; with it, its services and domain claims
// are deleted first.
func (client *Client) DeleteProject(name string, cascade bool) error {
	path := "/projects/" + name
	if cascade {
		path += "?cascade=true"
	}
	return client.request(http.MethodDelete, path, nil, nil)
}
