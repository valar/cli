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
func (client *Client) InspectTask(project, service, id string) (*Task, error) {
	var (
		task Task
		path = fmt.Sprintf("/%s/%s/task/%s/inspect", project, service, id)
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
		path  = fmt.Sprintf("/%s/%s/task/%s", project, service, id)
	)
	if err := client.request(http.MethodGet, path, &tasks, nil); err != nil {
		return nil, err
	}
	return tasks, nil
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
