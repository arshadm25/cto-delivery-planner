// Package redmine is a read-only client for the Redmine REST API.
//
// The client can only send GET requests. Any other method is refused before
// it leaves the process, so the planner cannot change Redmine data.
package redmine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrWriteRefused is returned when anything tries to send a non-GET request.
var ErrWriteRefused = errors.New("redmine: request refused, the client is read-only")

// Config holds connection settings. APIKey is sent only in the
// X-Redmine-API-Key header and is never included in URLs, logs or errors.
type Config struct {
	BaseURL    string
	APIKey     string
	PageSize   int
	Timeout    time.Duration
	MaxRetries int
	// AllowHTTP permits a plain http:// base URL. Only for local test servers.
	AllowHTTP bool
}

// Client talks to one Redmine instance.
type Client struct {
	base       *url.URL
	apiKey     string
	pageSize   int
	maxRetries int
	http       *http.Client
	backoff    func(attempt int) time.Duration
}

// NewClient validates the config and returns a read-only client.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("redmine: base URL is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("redmine: API key is required")
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("redmine: invalid base URL: %w", err)
	}
	if base.Scheme != "https" && !(cfg.AllowHTTP && base.Scheme == "http") {
		return nil, fmt.Errorf("redmine: base URL must use https, got %q", base.Scheme)
	}
	if base.User != nil {
		return nil, errors.New("redmine: base URL must not contain credentials")
	}
	if cfg.PageSize <= 0 {
		cfg.PageSize = 100
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	return &Client{
		base:       base,
		apiKey:     cfg.APIKey,
		pageSize:   cfg.PageSize,
		maxRetries: cfg.MaxRetries,
		http: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: readOnlyTransport{next: http.DefaultTransport},
		},
		backoff: func(attempt int) time.Duration {
			return time.Duration(1<<attempt) * 500 * time.Millisecond
		},
	}, nil
}

// readOnlyTransport refuses every method except GET.
type readOnlyTransport struct{ next http.RoundTripper }

func (t readOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, ErrWriteRefused
	}
	return t.next.RoundTrip(req)
}

// StatusError is a non-2xx response from Redmine.
type StatusError struct {
	Path   string
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("redmine: GET %s returned HTTP %d", e.Path, e.Status)
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// getJSON sends a GET for path with query and decodes the JSON body into out.
// Transient failures (network errors, 429, 5xx) are retried with backoff.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	u := *c.base
	u.Path = c.base.Path + path
	u.RawQuery = query.Encode()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.backoff(attempt - 1)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Redmine-API-Key", c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("redmine: GET %s: %w", path, unwrapURLError(err))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			lastErr = &StatusError{Path: path, Status: resp.StatusCode}
			if retryable(resp.StatusCode) {
				continue
			}
			return lastErr
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("redmine: decode %s: %w", path, err)
		}
		return nil
	}
	return lastErr
}

// unwrapURLError drops the url.Error wrapper so errors carry the path only.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// page is the envelope Redmine uses for paginated collections.
type page struct {
	TotalCount int `json:"total_count"`
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
}

// PageStats describes how a paginated listing went.
type PageStats struct {
	// ReportedTotal is total_count from the first page.
	ReportedTotal int
	// FinalTotal is total_count from the last page. It differs from
	// ReportedTotal when records were added or removed during the fetch.
	FinalTotal int
	Pages      int
	// Duplicates counts records seen more than once across pages, which
	// happens when data shifts under offset pagination.
	Duplicates int
}

// fetchAll walks every page of a collection. decode reads one page body and
// returns the envelope plus the IDs it added, so duplicates can be counted.
func (c *Client) fetchAll(ctx context.Context, path string, query url.Values,
	decode func(body json.RawMessage) (page, []int, error)) (PageStats, error) {

	var stats PageStats
	seen := map[int]bool{}
	offset := 0
	for {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(c.pageSize))

		var body json.RawMessage
		if err := c.getJSON(ctx, path, q, &body); err != nil {
			return stats, err
		}
		pg, ids, err := decode(body)
		if err != nil {
			return stats, fmt.Errorf("redmine: decode %s: %w", path, err)
		}
		stats.Pages++
		if stats.Pages == 1 {
			stats.ReportedTotal = pg.TotalCount
		}
		stats.FinalTotal = pg.TotalCount
		for _, id := range ids {
			if seen[id] {
				stats.Duplicates++
			}
			seen[id] = true
		}
		if len(ids) == 0 {
			return stats, nil
		}
		// Follow the server's own limit: it may cap the page size lower
		// than requested.
		step := pg.Limit
		if step <= 0 {
			step = len(ids)
		}
		offset = pg.Offset + step
		if offset >= pg.TotalCount {
			return stats, nil
		}
	}
}
