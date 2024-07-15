package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type JSONError struct {
	Err string `json:"error"`
}

func (err JSONError) Error() string {
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

type Error struct {
	StatusCode  int
	ServerError error
}

func (err Error) Error() string {
	return fmt.Sprintf("%s: %v", http.StatusText(err.StatusCode), err.ServerError)
}

func parseErrorResponse(body []byte) error {
	serverErr := JSONError{}
	if err := json.Unmarshal(body, &serverErr); err != nil {
		return fmt.Errorf("unmarshalling error response: %w", err)
	}
	return serverErr
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
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("fetching response: %w", err)
		}
		return Error{
			StatusCode:  resp.StatusCode,
			ServerError: parseErrorResponse(body),
		}
	}
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fetching request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Error{
			StatusCode:  resp.StatusCode,
			ServerError: parseErrorResponse(body),
		}
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

// StreamServiceLogs streams the logs of the latest service endpoint.
func (client *Client) StreamServiceLogs(project, service string, w io.Writer, follow, tail bool, skip int) error {
	params := url.Values{}
	if follow {
		params.Set("follow", "true")
	}
	if tail {
		params.Set("seek", "end")
	} else {
		params.Set("seek", "start")
	}
	params.Set("skip", strconv.Itoa(skip))
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/logs?%s", project, service, params.Encode())
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
func (client *Client) ListPermissions(project, namespace, prefix string) ([]Permission, error) {
	params := url.Values{}
	params.Add("namespace", namespace)
	params.Add("prefix", prefix)
	var (
		set  = []Permission{}
		path = fmt.Sprintf("/projects/%s/permissions?%s", project, params.Encode())
	)
	if err := client.request(http.MethodGet, path, &set, nil); err != nil {
		return nil, err
	}
	return set, nil
}

// ModifyPermission modifies the permission set of a project.
func (client *Client) ModifyPermission(project string, permission Permission) (bool, error) {
	var (
		resp struct {
			Modified bool `json:"modified"`
		}
		path   = fmt.Sprintf("/projects/%s/permissions", project)
		method = http.MethodPost
	)
	body, _ := json.Marshal(&permission)
	if err := client.request(method, path, &resp, bytes.NewReader(body)); err != nil {
		return false, err
	}
	return resp.Modified, nil
}

// CheckPermission checks if a user has a specific permission.
func (client *Client) CheckPermission(project string, permission Permission) (bool, error) {
	var (
		resp struct {
			Allowed bool `json:"allowed"`
		}
		path   = fmt.Sprintf("/projects/%s/permissions?mode=check", project)
		method = http.MethodPost
	)
	body, _ := json.Marshal(&permission)
	if err := client.request(method, path, &resp, bytes.NewReader(body)); err != nil {
		return false, err
	}
	return resp.Allowed, nil
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

func (client *Client) DeleteDomain(project, domain string) error {
	var (
		path = fmt.Sprintf("/projects/%s/domains/%s", project, domain)
	)
	if err := client.request(http.MethodDelete, path, nil, nil); err != nil {
		return err
	}
	return nil
}

type LinkDomainArgs struct {
	Project              string
	Domain               string
	Service              string
	AllowInsecureTraffic bool
}

func (client *Client) LinkDomain(args LinkDomainArgs) error {
	var (
		path       = fmt.Sprintf("/projects/%s/domains/%s/link", args.Project, args.Domain)
		payload, _ = json.Marshal(struct {
			Service              string `json:"service"`
			AllowInsecureTraffic bool   `json:"allowInsecureTraffic"`
		}{
			Service:              args.Service,
			AllowInsecureTraffic: args.AllowInsecureTraffic,
		})
	)
	if err := client.request(http.MethodPost, path, nil, bytes.NewReader(payload)); err != nil {
		return err
	}
	return nil
}

type UnlinkDomainArgs struct {
	Project string
	Domain  string
	Service string
}

func (client *Client) UnlinkDomain(args UnlinkDomainArgs) error {
	var (
		path       = fmt.Sprintf("/projects/%s/domains/%s/link", args.Project, args.Domain)
		payload, _ = json.Marshal(struct {
			Service string `json:"service"`
		}{
			Service: args.Service,
		})
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

func (client *Client) ListSchedules(project, service string) ([]Schedule, error) {
	var (
		path      = fmt.Sprintf("/projects/%s/services/%s/schedules", project, service)
		schedules []Schedule
	)
	if err := client.request(http.MethodGet, path, &schedules, nil); err != nil {
		return nil, err
	}
	return schedules, nil
}

func (client *Client) SetSchedule(project, service string, sched Schedule) error {
	var (
		path       = fmt.Sprintf("/projects/%s/services/%s/schedules", project, service)
		payload, _ = json.Marshal(sched)
	)
	return client.request(http.MethodPost, path, nil, bytes.NewReader(payload))
}

func (client *Client) DeleteSchedule(project, service, schedule string) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/schedules/%s", project, service, schedule)
	)
	return client.request(http.MethodDelete, path, nil, nil)
}

func (client *Client) TriggerSchedule(project, service, schedule string) error {
	var (
		path = fmt.Sprintf("/projects/%s/services/%s/schedules/%s/trigger", project, service, schedule)
	)
	return client.request(http.MethodPost, path, nil, nil)
}

func (client *Client) InspectSchedule(project, service, schedule string) (*ScheduleDetails, error) {
	var (
		path    = fmt.Sprintf("/projects/%s/services/%s/schedules/%s", project, service, schedule)
		details ScheduleDetails
	)
	if err := client.request(http.MethodGet, path, &details, nil); err != nil {
		return nil, err
	}
	return &details, nil
}

type Schedule struct {
	Name     string `json:"name"`
	Timespec string `json:"timespec"`
	Path     string `json:"path"`
	Payload  string `json:"payload"`
	Status   string `json:"status"`
}

type ScheduleDetails struct {
	LastRun  *ServiceInvocation `json:"invocation"`
	Schedule *Schedule          `json:"schedule"`
}

type ServiceInvocation struct {
	ID            string    `json:"id"`
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime,omitempty"`
	Status        string    `json:"status"`
	TriggerSource string    `json:"triggeredBy"`
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

type PermissionPath struct {
	Namespace string   `json:"namespace"`
	Items     []string `json:"items"`
}

func (pp PermissionPath) String() string {
	return fmt.Sprintf("%s:%s", pp.Namespace, strings.Join(pp.Items, "/"))
}

func PermissionPathFromString(str string) (PermissionPath, error) {
	path := PermissionPath{}
	parts := strings.SplitN(str, ":", 2)
	if len(parts) != 2 {
		return path, fmt.Errorf("invalid path: %s", str)
	}
	path.Namespace = parts[0]
	path.Items = strings.Split(parts[1], "/")
	return path, nil
}

type PermissionUser struct {
	Type       string   `json:"type"`
	Identifier []string `json:"identifier"`
}

func (pp PermissionUser) String() string {
	return fmt.Sprintf("%s:%s", pp.Type, strings.Join(pp.Identifier, "/"))
}

func PermissionUserFromString(str string) (PermissionUser, error) {
	user := PermissionUser{}
	parts := strings.SplitN(str, ":", 2)
	if len(parts) != 2 {
		return user, fmt.Errorf("invalid user: %s", str)
	}
	user.Type = parts[0]
	user.Identifier = strings.Split(parts[1], "/")
	return user, nil
}

type Permission struct {
	Path   PermissionPath `json:"path"`
	User   PermissionUser `json:"user"`
	Action string         `json:"action"`
	State  string         `json:"state"`
}

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
