package mpv

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var ErrClosed = errors.New("mpv client closed")

type response struct {
	raw map[string]json.RawMessage
	err error
}

type Client struct {
	conn net.Conn

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[int]chan response
	nextID  int

	events chan map[string]interface{}
	done   chan struct{}

	closeOnce sync.Once
}

func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial mpv socket %s: %w", socketPath, err)
	}

	c := &Client{
		conn:    conn,
		pending: make(map[int]chan response),
		events:  make(chan map[string]interface{}, 32),
		done:    make(chan struct{}),
	}
	go c.readLoop()

	return c, nil
}

func (c *Client) Close() error {
	return c.shutdown(ErrClosed, true)
}

func (c *Client) Command(args ...interface{}) error {
	resp, err := c.request(args...)
	if err != nil {
		return err
	}
	if resp.err != nil {
		return resp.err
	}

	if errMsg := mpvError(resp.raw); errMsg != "" && errMsg != "success" {
		return fmt.Errorf("mpv command failed: %s", errMsg)
	}
	return nil
}

func (c *Client) Get(prop string) (json.RawMessage, error) {
	resp, err := c.request("get_property", prop)
	if err != nil {
		return nil, err
	}
	if resp.err != nil {
		return nil, resp.err
	}

	if errMsg := mpvError(resp.raw); errMsg != "" && errMsg != "success" {
		return nil, fmt.Errorf("mpv get_property %q failed: %s", prop, errMsg)
	}

	data, ok := resp.raw["data"]
	if !ok {
		return nil, fmt.Errorf("mpv get_property %q response missing data", prop)
	}
	return append(json.RawMessage(nil), data...), nil
}

func (c *Client) Set(prop string, value interface{}) error {
	return c.Command("set_property", prop, value)
}

func (c *Client) LoadFile(path, mode string) error {
	return c.Command("loadfile", path, mode)
}

func (c *Client) InsertNext(path string) error {
	if err := c.LoadFile(path, "append"); err != nil {
		return err
	}

	posRaw, err := c.Get("playlist-pos")
	if err != nil {
		return err
	}
	pos, err := parseIntRaw(posRaw)
	if err != nil {
		return fmt.Errorf("parse playlist-pos: %w", err)
	}

	countRaw, err := c.Get("playlist-count")
	if err != nil {
		return err
	}
	count, err := parseIntRaw(countRaw)
	if err != nil {
		return fmt.Errorf("parse playlist-count: %w", err)
	}

	if count > 1 && pos >= 0 {
		return c.Command("playlist-move", count-1, pos+1)
	}
	return nil
}

func (c *Client) PlaylistCount() (int, error) {
	raw, err := c.Get("playlist-count")
	if err != nil {
		return 0, err
	}
	return parseIntRaw(raw)
}

func (c *Client) PlaylistPaths() ([]string, error) {
	raw, err := c.Get("playlist")
	if err != nil {
		return nil, err
	}

	var items []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse playlist: %w", err)
	}

	paths := make([]string, 0, len(items))
	for _, item := range items {
		if item.Filename != "" {
			paths = append(paths, item.Filename)
		}
	}
	return paths, nil
}

func (c *Client) Events() <-chan map[string]interface{} {
	return c.events
}

func WaitForSocket(socketPath string, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("timeout waiting for socket %s after %s", socketPath, timeout)
	}

	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for socket %s after %s", socketPath, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *Client) readLoop() {
	dec := json.NewDecoder(c.conn)

	for {
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			_ = c.shutdown(err, false)
			return
		}

		reqID, hasID := parseRequestID(msg)
		if hasID {
			c.mu.Lock()
			ch, ok := c.pending[reqID]
			if ok {
				delete(c.pending, reqID)
			}
			c.mu.Unlock()

			if ok {
				ch <- response{raw: msg}
				close(ch)
			}
			continue
		}

		event := make(map[string]interface{}, len(msg))
		for key, raw := range msg {
			var value interface{}
			if err := json.Unmarshal(raw, &value); err != nil {
				continue
			}
			event[key] = value
		}

		select {
		case c.events <- event:
		case <-c.done:
			return
		}
	}
}

func (c *Client) request(args ...interface{}) (response, error) {
	select {
	case <-c.done:
		return response{}, ErrClosed
	default:
	}

	ch := make(chan response, 1)

	c.mu.Lock()
	if c.pending == nil {
		c.mu.Unlock()
		return response{}, ErrClosed
	}
	c.nextID++
	reqID := c.nextID
	c.pending[reqID] = ch
	c.mu.Unlock()

	payload := map[string]interface{}{
		"command":    args,
		"request_id": reqID,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		c.removePending(reqID)
		return response{}, fmt.Errorf("marshal mpv command: %w", err)
	}

	c.writeMu.Lock()
	_, err = c.conn.Write(append(b, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		c.removePending(reqID)
		return response{}, fmt.Errorf("write mpv command: %w", err)
	}

	resp, ok := <-ch
	if !ok {
		return response{}, ErrClosed
	}
	return resp, nil
}

func (c *Client) removePending(reqID int) {
	c.mu.Lock()
	ch, ok := c.pending[reqID]
	if ok {
		delete(c.pending, reqID)
	}
	c.mu.Unlock()

	if ok {
		ch <- response{err: ErrClosed}
		close(ch)
	}
}

func (c *Client) shutdown(reason error, closeConn bool) error {
	var closeErr error

	c.closeOnce.Do(func() {
		if closeConn {
			closeErr = c.conn.Close()
		}

		c.mu.Lock()
		pending := c.pending
		c.pending = nil
		c.mu.Unlock()

		for _, ch := range pending {
			ch <- response{err: reason}
			close(ch)
		}

		close(c.done)
		close(c.events)
	})

	return closeErr
}

func parseRequestID(msg map[string]json.RawMessage) (int, bool) {
	raw, ok := msg["request_id"]
	if !ok {
		return 0, false
	}

	var i int
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, true
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), true
	}

	return 0, false
}

func parseIntRaw(raw json.RawMessage) (int, error) {
	var i int
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, nil
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), nil
	}

	return 0, fmt.Errorf("unsupported numeric value: %s", string(raw))
}

func mpvError(msg map[string]json.RawMessage) string {
	raw, ok := msg["error"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
