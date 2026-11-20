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

// DeleteProject deletes an empty project. The platform retires its name for good.
func (client *Client) DeleteProject(name string) error {
	return client.request(http.MethodDelete, "/projects/"+name, nil, nil)
}
