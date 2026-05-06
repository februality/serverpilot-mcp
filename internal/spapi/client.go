package spapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultBaseURL = "https://api.serverpilot.io/v1"

type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ServerPilot API %s %s failed (%d): %s", e.Method, e.Path, e.Status, e.Body)
}

type Client struct {
	baseURL    string
	authHeader string
	http       *http.Client
}

func NewClient(clientID, apiKey string) *Client {
	return NewClientWithBaseURL(clientID, apiKey, DefaultBaseURL)
}

func NewClientWithBaseURL(clientID, apiKey, baseURL string) *Client {
	creds := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + apiKey))
	return &Client{
		baseURL:    baseURL,
		authHeader: "Basic " + creds,
		http:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) request(method, path string, body any, out any) error {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ServerPilot API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: string(respBody)}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) Get(path string, out any) error {
	return c.request(http.MethodGet, path, nil, out)
}

func (c *Client) Post(path string, body any, out any) error {
	return c.request(http.MethodPost, path, body, out)
}

func (c *Client) Delete(path string, out any) error {
	return c.request(http.MethodDelete, path, nil, out)
}
