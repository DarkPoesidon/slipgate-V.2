package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.cloudflare.com/client/v4"

type Client struct {
	token      string
	httpClient *http.Client
}

type Record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied *bool  `json:"proxied,omitempty"`
}

type apiResponse[T any] struct {
	Success bool `json:"success"`
	Result  T    `json:"result"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func New(token string) *Client {
	return &Client{
		token: strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) ZoneID(ctx context.Context, zoneName string) (string, error) {
	zoneName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zoneName)), ".")
	if zoneName == "" {
		return "", fmt.Errorf("zone name is required")
	}

	var out apiResponse[[]zone]
	path := "/zones?name=" + url.QueryEscape(zoneName) + "&status=active"
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	if len(out.Result) == 0 {
		return "", fmt.Errorf("Cloudflare zone %q not found", zoneName)
	}
	return out.Result[0].ID, nil
}

func (c *Client) Records(ctx context.Context, zoneID, name string) ([]Record, error) {
	var out apiResponse[[]Record]
	path := fmt.Sprintf("/zones/%s/dns_records?name=%s", url.PathEscape(zoneID), url.QueryEscape(strings.TrimSuffix(name, ".")))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

func (c *Client) CreateRecord(ctx context.Context, zoneID string, rec Record) error {
	var out apiResponse[Record]
	path := fmt.Sprintf("/zones/%s/dns_records", url.PathEscape(zoneID))
	return c.do(ctx, http.MethodPost, path, rec, &out)
}

func (c *Client) UpdateRecord(ctx context.Context, zoneID string, rec Record) error {
	if rec.ID == "" {
		return fmt.Errorf("record ID is required")
	}
	var out apiResponse[Record]
	path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zoneID), url.PathEscape(rec.ID))
	return c.do(ctx, http.MethodPatch, path, rec, &out)
}

func (c *Client) do(ctx context.Context, method, path string, body any, target any) error {
	if c.token == "" {
		return fmt.Errorf("Cloudflare API token is required")
	}

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cloudflare API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}

	success, errorsText := responseStatus(target)
	if !success {
		return fmt.Errorf("Cloudflare API error: %s", errorsText)
	}
	return nil
}

func responseStatus(target any) (bool, string) {
	data, err := json.Marshal(target)
	if err != nil {
		return false, err.Error()
	}
	var raw struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, err.Error()
	}
	if raw.Success {
		return true, ""
	}
	var parts []string
	for _, e := range raw.Errors {
		if e.Message != "" {
			parts = append(parts, e.Message)
		}
	}
	if len(parts) == 0 {
		return false, "unknown error"
	}
	return false, strings.Join(parts, "; ")
}
