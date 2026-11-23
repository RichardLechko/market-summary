// Package httpx is a small HTTP client with retries, a fixed User-Agent, and
// an optional minimum interval between requests (for SEC rate limits).
package httpx

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	URL    string
	Status int
}

func (e *StatusError) Error() string { return fmt.Sprintf("GET %s: HTTP %d", e.URL, e.Status) }

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
	var lastErr error
	backoff := c.BaseBackoff
	for attempt := 1; attempt <= c.Attempts; attempt++ {
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		body, status, err := c.do(ctx, url)
		switch {
		case err == nil && status >= 200 && status < 300:
			return body, nil
		case err == nil && status != http.StatusTooManyRequests && status < 500:
			return nil, &StatusError{URL: url, Status: status}
		case err == nil:
			lastErr = &StatusError{URL: url, Status: status}
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

func (c *Client) do(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
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
