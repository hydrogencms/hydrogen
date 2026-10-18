package meilisearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// client is the part of Meilisearch's HTTP API the module uses.
type client struct {
	base    string
	key     string
	hc      *http.Client
	timeout time.Duration // per HTTP request
	// taskTimeout bounds the wait for one task.
	taskTimeout time.Duration
}

// apiError is an error answer of the server.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("meilisearch: %s (%s, HTTP %d)", e.Message, e.Code, e.Status)
	}
	return fmt.Sprintf("meilisearch: %s (HTTP %d)", e.Message, e.Status)
}

func isCode(err error, code string) bool {
	var ae *apiError
	var te *taskError
	return (errors.As(err, &ae) && ae.Code == code) || (errors.As(err, &te) && te.Code == code)
}

// do sends one request; in is encoded as JSON, out (if not nil) decoded from the answer.
func (c *client) do(ctx context.Context, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("meilisearch: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("meilisearch: %s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		ae := &apiError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		var e struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		if json.Unmarshal(data, &e) == nil && e.Message != "" {
			ae.Message, ae.Code = e.Message, e.Code
		}
		return ae
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("meilisearch: %s %s: bad answer: %w", method, path, err)
		}
	}
	return nil
}

type task struct {
	UID    int64  `json:"uid"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// run sends a write request and waits for its task. The wait is bounded by the timeout.
func (c *client) run(ctx context.Context, method, path string, in any) error {
	var enq struct {
		TaskUID int64 `json:"taskUid"`
	}
	if err := c.do(ctx, method, path, in, &enq); err != nil {
		return err
	}
	return c.wait(ctx, enq.TaskUID)
}

func (c *client) wait(ctx context.Context, uid int64) error {
	ctx, cancel := context.WithTimeout(ctx, c.taskTimeout)
	defer cancel()
	delay := 5 * time.Millisecond
	for {
		var t task
		if err := c.do(ctx, http.MethodGet, "/tasks/"+fmt.Sprint(uid), nil, &t); err != nil {
			return err
		}
		switch t.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			msg := t.Status
			code := ""
			if t.Error != nil {
				msg, code = t.Error.Message, t.Error.Code
			}
			return &taskError{UID: uid, Status: t.Status, Code: code, Message: msg}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("meilisearch: task %d still %s after %s: %w", uid, t.Status, c.taskTimeout, ctx.Err())
		case <-time.After(delay):
		}
		delay = min(delay*2, 250*time.Millisecond)
	}
}

// taskError is a task that failed or was canceled.
type taskError struct {
	UID     int64
	Status  string
	Code    string
	Message string
}

func (e *taskError) Error() string {
	return fmt.Sprintf("meilisearch: task %d %s: %s", e.UID, e.Status, e.Message)
}

func idxPath(uid string) string { return "/indexes/" + url.PathEscape(uid) }
