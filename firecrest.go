// Package firecrest provides a small, context-aware client for FirecREST v2.
package firecrest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Client talks to one FirecREST installation and one configured HPC system.
type Client struct {
	baseURL *url.URL
	system  string
	http    *http.Client
}

// NewClient makes a client. httpClient must provide authentication (for example
// a client returned by NewClientCredentialsHTTPClient).
func NewClient(baseURL, system string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q", baseURL)
	}
	if system == "" {
		return nil, fmt.Errorf("system must not be empty")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: u, system: system, http: httpClient}, nil
}

// SubmitRequest is the JSON body accepted by POST /compute/{system}/jobs.
type SubmitRequest struct {
	Job JobDescription `json:"job"`
}

// JobDescription is deliberately limited to portable submission fields.
type JobDescription struct {
	Script           string            `json:"script"`
	WorkingDirectory string            `json:"working_directory"`
	Name             string            `json:"name,omitempty"`
	Account          string            `json:"account,omitempty"`
	Partition        string            `json:"partition,omitempty"`
	StandardOutput   string            `json:"standard_output,omitempty"`
	StandardError    string            `json:"standard_error,omitempty"`
	Environment      map[string]string `json:"env,omitempty"`
}

// Submit submits a job and returns its scheduler identifier.
func (c *Client) Submit(ctx context.Context, request SubmitRequest) (string, error) {
	var response struct {
		JobID json.RawMessage `json:"jobId"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "compute", "jobs", request, &response); err != nil {
		return "", err
	}
	var id string
	if err := json.Unmarshal(response.JobID, &id); err == nil {
		return id, nil
	}
	var number json.Number
	if err := json.Unmarshal(response.JobID, &number); err != nil {
		return "", fmt.Errorf("decode jobId: %w", err)
	}
	return number.String(), nil
}

// JobMetadata contains the script and standard streams returned in one v2 call.
type JobMetadata struct {
	JobID          string `json:"jobId"`
	Script         string `json:"script"`
	StandardInput  string `json:"standardInput"`
	StandardOutput string `json:"standardOutput"`
	StandardError  string `json:"standardError"`
}

// JobStatus returns metadata for a job, including standard output and error paths.
func (c *Client) JobStatus(ctx context.Context, jobID string) (JobMetadata, error) {
	var response struct {
		Jobs []JobMetadata `json:"jobs"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "compute", path.Join("jobs", jobID, "metadata"), nil, &response); err != nil {
		return JobMetadata{}, err
	}
	if len(response.Jobs) != 1 {
		return JobMetadata{}, fmt.Errorf("decode job metadata: expected one job, got %d", len(response.Jobs))
	}
	return response.Jobs[0], nil
}

// Download fetches a small file through the synchronous filesystem ops endpoint.
// The caller owns closing the returned body.
func (c *Client) Download(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	req, err := c.request(ctx, http.MethodGet, "filesystem", "ops/download", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("path", remotePath)
	req.URL.RawQuery = q.Encode()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FirecREST request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newAPIError(resp)
	}
	return resp.Body, nil
}

// Upload sends a small file through the synchronous multipart ops endpoint.
func (c *Client) Upload(ctx context.Context, directory, filename string, content io.Reader) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err == nil {
		_, err = io.Copy(part, content)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return fmt.Errorf("build upload: %w", err)
	}
	req, err := c.request(ctx, http.MethodPost, "filesystem", "ops/upload", &body)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("path", directory)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("FirecREST request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, area, endpoint string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := c.request(ctx, method, area, endpoint, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("FirecREST request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, area, endpoint string, body io.Reader) (*http.Request, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/" + path.Join(area, c.system, endpoint)
	return http.NewRequestWithContext(ctx, method, u.String(), body)
}
