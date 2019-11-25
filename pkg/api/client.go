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

func (client *Client) error(clientErr error, body []byte) error {
	serverErr := Error{}
	if err := json.Unmarshal(body, &serverErr); err != nil {
		return fmt.Errorf("client: %w: %v", clientErr, string(body))
	}
	return fmt.Errorf("server: %w", serverErr)
}

func (client *Client) streamRequest(method, path string, w io.Writer) error {
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
	if err := json.Unmarshal(body, obj); err != nil {
		return client.error(err, body)
	}
	return nil
}

// ListServices returns a list of all services in the project with the given prefix.
func (client *Client) ListServices(project, service string) ([]Service, error) {
	var (
		services []Service
		path     = fmt.Sprintf("/%s/%s", project, service)
	)
	if err := client.request(http.MethodGet, path, &services, nil); err != nil {
		return nil, err
	}
	return services, nil
}

// ShowServiceLogs fetches the up-to-date logs of the latest service endpoint.
func (client *Client) ShowServiceLogs(project, service string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/%s/%s/logs", project, service)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// StreamServiceLogs streams the logs of the latest service endpoint.
func (client *Client) StreamServiceLogs(project, service string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/%s/%s/logs?follow=true", project, service)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// SubmitBuild submits a new build task to the server.
func (client *Client) SubmitBuild(project, function, constructor string, archive io.ReadCloser) (*Task, error) {
	var (
		task Task
		path = fmt.Sprintf("/%s/%s/build?env=%s", project, function, constructor)
	)
	if err := client.request(http.MethodPost, path, &task, archive); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetBuild retrieves a specific build task.
func (client *Client) ShowTaskLogs(project, service, id string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/%s/%s/tasks/%s/logs", project, service, id)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

func (client *Client) StreamTaskLogs(project, service, id string, w io.Writer) error {
	var (
		path = fmt.Sprintf("/%s/%s/tasks/%s/logs?follow=true", project, service, id)
	)
	if err := client.streamRequest(http.MethodGet, path, w); err != nil {
		return err
	}
	return nil
}

// GetBuild retrieves a specific build task.
func (client *Client) InspectTask(project, service, id string) (*Task, error) {
	var (
		task Task
		path = fmt.Sprintf("/%s/%s/tasks/%s/inspect", project, service, id)
	)
	if err := client.request(http.MethodGet, path, &task, nil); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListBuilds retrieves all builds for a specific service.
func (client *Client) ListTasks(project, service, id string) ([]Task, error) {
	var (
		tasks []Task
		path  = fmt.Sprintf("/%s/%s/tasks/%s", project, service, id)
	)
	if err := client.request(http.MethodGet, path, &tasks, nil); err != nil {
		return nil, err
	}
	return tasks, nil
}

type Service struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Version string `json:"version"`
}

type Task struct {
	ID      string    `json:"id"`
	Label   string    `json:"label"`
	Status  string    `json:"status"`
	Domain  string    `json:"domain"`
	Created time.Time `json:"created"`
	Logs    string    `json:"logs"`
	Err     string    `json:"error"`
}

func NewClient(endpoint, token string) *Client {
	return &Client{
		Endpoint: endpoint,
		Token:    token,
		http: &http.Client{
			Timeout: time.Minute,
		},
	}
}
