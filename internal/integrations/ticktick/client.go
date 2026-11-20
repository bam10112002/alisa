package ticktick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://api.ticktick.com/open/v1"
const maxResponseBytes = 8 << 20

var ErrNotConfigured = errors.New("TickTick token is not configured")

// APIError deliberately excludes response bodies, which may contain private data.
type APIError struct {
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("TickTick returned HTTP %d", e.StatusCode)
}

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string, timeout time.Duration) *Client {
	return &Client{
		token: strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	projects := make([]Project, 0)
	if err := c.get(ctx, "/project", &projects); err != nil {
		return nil, err
	}
	if projects == nil {
		projects = []Project{}
	}
	return projects, nil
}

func (c *Client) ListProjectTasks(ctx context.Context, projectID string) ([]Task, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("project ID must not be empty")
	}
	var data struct {
		Tasks []Task `json:"tasks"`
	}
	if err := c.get(ctx, "/project/"+url.PathEscape(projectID)+"/data", &data); err != nil {
		return nil, err
	}
	if data.Tasks == nil {
		data.Tasks = []Task{}
	}
	return data.Tasks, nil
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	if c.token == "" {
		return ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create TickTick request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request TickTick: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &APIError{StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read TickTick response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return errors.New("TickTick response exceeds size limit")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode TickTick response: %w", err)
	}
	return nil
}
