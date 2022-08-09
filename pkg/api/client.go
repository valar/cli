package api

import (
	"bytes"
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

func (client *Client) UserInfo() (*UserInfo, error) {
	var userInfo UserInfo
	if err := client.request(http.MethodGet, "/users/info", &userInfo, nil); err != nil {
		return nil, err
	}
	return &userInfo, nil
}

func (client *Client) check() error {
	var resp struct {
		Version string `json:"version"`
	}
	if err := client.request(http.MethodGet, "/", &resp, nil); err != nil {
		return err
	}
	if resp.Version != "v2" {
		return fmt.Errorf("client requires endpoint v2")
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
	if obj != nil {
		if err := json.Unmarshal(body, obj); err != nil {
			return fmt.Errorf("unmarshalling response: %w", err)
		}
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

// SubmitArtifact submits a new build input artifact to the server.
func (client *Client) SubmitArtifact(project, service string, archive io.ReadCloser) (*Artifact, error) {
	var (
		artifact Artifact
		path     = fmt.Sprintf("/projects/%s/services/%s/artifacts", project, service)
	)
	if err := client.request(http.MethodPost, path, &artifact, archive); err != nil {
		return nil, err
	}
	return &artifact, nil
}

// SubmitBuild submits a new build task to the server.
func (client *Client) SubmitBuild(project, service string, buildRequest *BuildRequest) (*Build, error) {
	var (
		payload, _ = json.Marshal(buildRequest)
		build      Build
		path       = fmt.Sprintf("/projects/%s/services/%s/builds", project, service)
	)
	if err := client.request(http.MethodPost, path, &build, bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	return &build, nil
}

// AbortBuild aborts a scheduled or running build.
func (client *Client) AbortBuild(project, service, id string) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/builds/%s/abort", project, service, id)
	)
	var resp struct{}
	if err := client.request("POST", path, &resp, nil); err != nil {
		return err
	}
	return nil
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
		path  = fmt.Sprintf("/projects/%s/services/%s/deploys", project, service)
	)
	if err := client.request(http.MethodGet, path, &depls, nil); err != nil {
		return nil, err
	}
	return depls, nil
}

// SubmitDeploy submits a new deployment of the given build.
func (client *Client) SubmitDeploy(project, service string, deploy *DeployRequest) (*Deployment, error) {
	var (
		payload, _ = json.Marshal(deploy)
		depl       Deployment
		path       = fmt.Sprintf("/projects/%s/services/%s/deploys", project, service)
	)
	if err := client.request(http.MethodPost, path, &depl, bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	return &depl, nil
}

// RollbackDeploy rolls back to a previous deployment.
func (client *Client) RollbackDeploy(project, service string, rollback *RollbackRequest) (*Deployment, error) {
	var (
		payload, _ = json.Marshal(rollback)
		depl       Deployment
		path       = fmt.Sprintf("/projects/%s/services/%s/deploys/rollback", project, service)
	)
	if err := client.request(http.MethodPost, path, &depl, bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	return &depl, nil
}

func (client *Client) EncryptEnvironment(project, service string, kvpair *KVPair) (*KVPair, error) {
	var (
		payload, _ = json.Marshal(kvpair)
		encrypted  KVPair
		path       = fmt.Sprintf("/projects/%s/environment/encrypt", project)
	)
	if err := client.request(http.MethodPost, path, &encrypted, bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	return &encrypted, nil
}

func (client *Client) ListDomains(project string) ([]Domain, error) {
	var (
		path    = fmt.Sprintf("/projects/%s/domains", project)
		domains []Domain
	)
	if err := client.request(http.MethodGet, path, &domains, nil); err != nil {
		return nil, err
	}
	return domains, nil
}

func (client *Client) AddDomain(project, domain string) (map[string]string, error) {
	var (
		records    = make(map[string]string)
		path       = fmt.Sprintf("/projects/%s/domains", project)
		payload, _ = json.Marshal(struct {
			Domain string `json:"domain"`
		}{domain})
	)
	if err := client.request(http.MethodPost, path, &records, bytes.NewReader(payload)); err != nil {
		return nil, err
	}
	return records, nil
}

func (client *Client) RemoveDomain(project, domain string) error {
	var (
		path = fmt.Sprintf("/projects/%s/domains/%s", project, domain)
	)
	if err := client.request(http.MethodDelete, path, nil, nil); err != nil {
		return err
	}
	return nil
}

func (client *Client) LinkDomain(project, domain, service string) error {
	var (
		path       = fmt.Sprintf("/projects/%s/domains/%s/link", project, domain)
		payload, _ = json.Marshal(struct {
			Service string `json:"service"`
		}{service})
	)
	if err := client.request(http.MethodPost, path, nil, bytes.NewReader(payload)); err != nil {
		return err
	}
	return nil
}

func (client *Client) UnlinkDomain(project, domain, service string) error {
	var (
		path       = fmt.Sprintf("/projects/%s/domains/%s/link", project, domain)
		payload, _ = json.Marshal(struct {
			Service string `json:"service"`
		}{service})
	)
	if err := client.request(http.MethodDelete, path, nil, bytes.NewReader(payload)); err != nil {
		return err
	}
	return nil
}

func (client *Client) VerifyDomain(project, domain string) (*Domain, error) {
	var (
		path = fmt.Sprintf("/projects/%s/domains/%s/verify", project, domain)
		dom  Domain
	)
	if err := client.request(http.MethodPost, path, &dom, nil); err != nil {
		return nil, err
	}
	return &dom, nil
}

type Domain struct {
	Project    string    `json:"project"`
	Domain     string    `json:"domain"`
	Token      string    `json:"token"`
	Verified   bool      `json:"verified"`
	Expiration time.Time `json:"expiration"`
	Error      string    `json:"error"`
	Service    *string   `json:"service"`
}

type RollbackRequest struct {
	Version int64 `json:"version"`
}

type Artifact struct {
	Artifact string `json:"artifact"`
}

type DeployRequest struct {
	Build       string   `json:"build,omitempty"`
	Environment []KVPair `json:"environment"`
}

type BuildRequest struct {
	Artifact string `json:"artifact"`
	Build    struct {
		Constructor string   `json:"constructor"`
		Environment []KVPair `json:"environment"`
	} `json:"build"`
	Deployment struct {
		Skip        bool     `json:"skip"`
		Environment []KVPair `json:"environment"`
	} `json:"deployment"`
}

type KVPair struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type PermissionSet map[string][]string

type UserInfo struct {
	Name     string   `json:"name"`
	Projects []string `json:"projects"`
}

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
