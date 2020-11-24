package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"
)

type Error struct {
	Err string `json:"error"`
}

func (err Error) Error() string {
	return err.Err
}

type Client struct {
	Endpoint string
	Token    string

	http *http.Client
}

func (client *Client) check() error {
	var resp struct {
		Version string `json:"version"`
	}
	client.request(http.MethodGet, "/", &resp, nil)
	if resp.Version != "v1" {
		return fmt.Errorf("client requires endpoint v1")
	}
	return nil
}

func (client *Client) error(clientErr error, body []byte) error {
	serverErr := Error{}
	if err := json.Unmarshal(body, &serverErr); err != nil {
		return fmt.Errorf("client: %w: %v", clientErr, string(body))
	}
	return fmt.Errorf("server: %w", serverErr)
}

func (client *Client) streamRequest(method, path string, w io.Writer) error {
	client.http.Timeout = 0
	req, err := http.NewRequest(method, client.Endpoint+path, nil)
	if err != nil {
		return fmt.Errorf("client request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+client.Token)
	resp, err := client.http.Do(req)
	if err != nil {
		return fmt.Errorf("submitting request: %w", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(w, resp.Body); err != nil && err != io.EOF {
		return fmt.Errorf("copying request: %w", err)
	}
	return nil
}

func (client *Client) request(method, path string, obj interface{}, post io.Reader) error {
	client.http.Timeout = time.Minute
	req, err := http.NewRequest(method, client.Endpoint+path, post)
	if err != nil {
		return fmt.Errorf("client request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+client.Token)
	resp, err := client.http.Do(req)
	if err != nil {
		return fmt.Errorf("submitting request: %w", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fetching request: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		// Do nothing, we're fine here
	case http.StatusNotFound:
		return fmt.Errorf("not found")
	default:
		return client.error(fmt.Errorf("bad response"), body)
	}
	if err := json.Unmarshal(body, obj); err != nil {
		return fmt.Errorf("unmarshalling response: %w", err)
	}
	return nil
}

// ListServices returns a list of all services in the project with the given prefix.
func (client *Client) ListServices(project, service string) ([]Service, error) {
	var (
		services []Service
		path     = fmt.Sprintf("/projects/%s/services/%s", project, service)
	)
	if err := client.request(http.MethodGet, path, &services, nil); err != nil {
		return nil, err
	}
	return services, nil
}

// ShowServiceLogs fetches the up-to-date logs of the latest service endpoint.
func (client *Client) ShowServiceLogs(project, service string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/logs", project, service)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// StreamServiceLogs streams the logs of the latest service endpoint.
func (client *Client) StreamServiceLogs(project, service string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/logs?follow=true", project, service)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// SubmitBuild submits a new build task to the server.
func (client *Client) SubmitBuild(project, service, constructor string, archive io.ReadCloser) (*Build, error) {
	var (
		task Build
		path = fmt.Sprintf("/projects/%s/services/%s/builds?constructor=%s", project, service, constructor)
	)
	if err := client.request(http.MethodPost, path, &task, archive); err != nil {
		return nil, err
	}
	return &task, nil
}

// ShowBuildLogs retrieves a specific build task logs.
func (client *Client) ShowBuildLogs(project, service, id string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/builds/%s/logs", project, service, id)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// StreamBuildLogs streams the active build logs to stdout.
func (client *Client) StreamBuildLogs(project, service, id string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/builds/%s/logs?follow=true", project, service, id)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// InspectBuild retrieves a specific build task.
func (client *Client) InspectBuild(project, service, id string) (*Build, error) {
	var (
		task Build
		path = fmt.Sprintf("/projects/%s/services/%s/builds/%s/inspect", project, service, id)
	)
	if err := client.request(http.MethodGet, path, &task, nil); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListBuilds retrieves all builds for a specific service.
func (client *Client) ListBuilds(project, service, id string) ([]Build, error) {
	var (
		tasks []Build
		path  = fmt.Sprintf("/projects/%s/services/%s/builds/%s", project, service, id)
	)
	if err := client.request(http.MethodGet, path, &tasks, nil); err != nil {
		return nil, err
	}
	return tasks, nil
}

// ListPermissions retrieves all permissions set for a specific project.
func (client *Client) ListPermissions(project string) (PermissionSet, error) {
	var (
		set  = make(PermissionSet)
		path = fmt.Sprintf("/projects/%s/auth", project)
	)
	if err := client.request(http.MethodGet, path, &set, nil); err != nil {
		return nil, err
	}
	return set, nil
}

// ModifyPermission modifies the permission set of a project.
func (client *Client) ModifyPermission(project, user, action string, forbid bool) error {
	var (
		resp   struct{}
		path   = fmt.Sprintf("/projects/%s/auth/%s/%s", project, user, action)
		method = http.MethodPost
	)
	if forbid {
		method = http.MethodDelete
	}
	if err := client.request(method, path, &resp, nil); err != nil {
		return err
	}
	return nil
}

// ListDeployments shows all deployments of a specific service.
func (client *Client) ListDeployments(project, service string) ([]Deployment, error) {
	var (
		depls []Deployment
		path  = fmt.Sprintf("/projects/%s/services/%s/deployments", project, service)
	)
	if err := client.request(http.MethodGet, path, &depls, nil); err != nil {
		return nil, err
	}
	return depls, nil
}

// SubmitDeploy submits a new deployment of the given build.
func (client *Client) SubmitDeploy(project, service, build string) (*Deployment, error) {
	var (
		depl Deployment
		path = fmt.Sprintf("/projects/%s/services/%s/builds/%s/deploy", project, service, build)
	)
	if err := client.request(http.MethodPost, path, &depl, nil); err != nil {
		return nil, err
	}
	return &depl, nil
}

type PermissionSet map[string][]string

type Service struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Deployment int64     `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	DeployedAt time.Time `json:"deployedAt"`
	Domains    []string  `json:"domains"`
}

type Build struct {
	ID          string    `json:"id"`
	Constructor string    `json:"constructor"`
	Status      string    `json:"status"`
	Err         string    `json:"error"`
	CreatedAt   time.Time `json:"createdAt"`
	Flags       string    `json:"flags"`
	Owner       string    `json:"owner"`
}

type Deployment struct {
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Error     string    `json:"error"`
	Status    string    `json:"status"`
	Build     string    `json:"build"`
}

// NewClient creates a new client instance.
func NewClient(endpoint, token string) (*Client, error) {
	client := &Client{
		Endpoint: endpoint,
		Token:    token,
		http: &http.Client{
			Timeout: time.Minute,
		},
	}
	if err := client.check(); err != nil {
		return nil, err
	}
	return client, nil
}
