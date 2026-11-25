// Package httpx is a small HTTP client with retries, a fixed User-Agent, and
// an optional minimum interval between requests (for SEC rate limits).
package httpx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client wraps http.Client.
type Client struct {
	HTTP        *http.Client
	UserAgent   string
	MinInterval time.Duration
	Attempts    int
	BaseBackoff time.Duration
	// Header is added to every request, e.g. an API key that must not
	// appear in URLs (URLs end up in error messages and logs).
	Header http.Header

	mu   sync.Mutex
	last time.Time
}

// New returns a client with sane defaults.
func New(userAgent string, minInterval time.Duration) *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 60 * time.Second},
		UserAgent:   userAgent,
		MinInterval: minInterval,
		Attempts:    3,
		BaseBackoff: time.Second,
	}
}

// StatusError is returned for non-2xx responses after retries.
type StatusError struct {
	Method string // empty means GET
	URL    string
	Status int
	Detail string // start of the response body, for APIs that explain errors
}

func (e *StatusError) Error() string {
	m := e.Method
	if m == "" {
		m = http.MethodGet
	}
	s := fmt.Sprintf("%s %s: HTTP %d", m, e.URL, e.Status)
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

func (c *Client) wait(ctx context.Context) error {
	if c.MinInterval <= 0 {
		return nil
	}
	c.mu.Lock()
	next := c.last.Add(c.MinInterval)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	c.last = next
	c.mu.Unlock()
	select {
	case <-time.After(time.Until(next)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Get fetches url and returns the body. It retries on network errors, 429,
// and 5xx with exponential backoff.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	return c.send(ctx, http.MethodGet, url, nil)
}

// PostJSON sends body as JSON and returns the response body, with the same
// retries as Get.
func (c *Client) PostJSON(ctx context.Context, url string, body []byte) ([]byte, error) {
	return c.send(ctx, http.MethodPost, url, body)
}

func (c *Client) send(ctx context.Context, method, url string, reqBody []byte) ([]byte, error) {
	var lastErr error
	backoff := c.BaseBackoff
	for attempt := 1; attempt <= c.Attempts; attempt++ {
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		body, status, err := c.do(ctx, method, url, reqBody)
		switch {
		case err == nil && status >= 200 && status < 300:
			return body, nil
		case err == nil && status != http.StatusTooManyRequests && status < 500:
			return nil, &StatusError{Method: method, URL: url, Status: status, Detail: detail(body)}
		case err == nil:
			lastErr = &StatusError{Method: method, URL: url, Status: status, Detail: detail(body)}
		default:
			lastErr = err
		}
		if attempt < c.Attempts {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			backoff *= 2
		}
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, method, url string, reqBody []byte) ([]byte, int, error) {
	var rd io.Reader
	if reqBody != nil {
		rd = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, 0, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	for k, v := range c.Header {
		req.Header[k] = v
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// detail returns the start of an error body on one line.
func detail(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
