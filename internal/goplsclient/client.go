package goplsclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"rat/internal/goplsbin"
)

type Location struct {
	File   string
	Line   int
	Column int
}

type Client struct {
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	nextID int
	opened map[string]openDocument
}

type openDocument struct {
	version int
	content string
}

var (
	defaultOnce sync.Once
	defaultInst *Client
	defaultErr  error
)

func Default() (*Client, error) {
	defaultOnce.Do(func() {
		defaultInst, defaultErr = start()
	})
	return defaultInst, defaultErr
}

func start() (*Client, error) {
	goplsBin, err := resolveGoplsBinary()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(goplsBin, "serve")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{stdin: stdin, stdout: bufio.NewReader(stdout), opened: map[string]openDocument{}}
	if err := c.initialize(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	return c, nil
}

func resolveGoplsBinary() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("GOPLS_BIN")); custom != "" {
		return custom, nil
	}
	if path, err := goplsbin.Path(); err == nil {
		return path, nil
	}
	if _, err := os.Stat("./gopls"); err == nil {
		return "./gopls", nil
	}
	path, err := exec.LookPath("gopls")
	if err != nil {
		return "", fmt.Errorf("gopls not found; set GOPLS_BIN, build the embedded gopls artifact, or include gopls in PATH")
	}
	return path, nil
}

func (c *Client) initialize() error {
	params := map[string]any{
		"processId":    os.Getpid(),
		"clientInfo":   map[string]any{"name": "rat", "version": "dev"},
		"capabilities": map[string]any{},
	}
	if _, err := c.request("initialize", params); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *Client) Hover(file string, line, column int) (string, error) {
	if err := c.syncDocument(file); err != nil {
		return "", err
	}
	uri := fileURI(file)
	result, err := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line - 1, "character": column - 1},
	})
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (c *Client) Definition(file string, line, column int) (Location, bool, error) {
	if err := c.syncDocument(file); err != nil {
		return Location{}, false, err
	}
	uri := fileURI(file)
	result, err := c.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line - 1, "character": column - 1},
	})
	if err != nil {
		return Location{}, false, err
	}
	loc, ok, err := parseDefinition(result)
	if err != nil {
		return Location{}, false, err
	}
	return loc, ok, nil
}

func (c *Client) syncDocument(file string) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	opened, ok := c.opened[abs]
	if !ok {
		if err := writeMessage(c.stdin, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
			"textDocument": map[string]any{
				"uri":        fileURI(abs),
				"languageId": "go",
				"version":    1,
				"text":       string(content),
			},
		}}); err != nil {
			return err
		}
		c.opened[abs] = openDocument{version: 1, content: string(content)}
		return nil
	}

	if opened.content == string(content) {
		return nil
	}

	opened.version++
	opened.content = string(content)
	if err := writeMessage(c.stdin, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
		"textDocument": map[string]any{
			"uri":     fileURI(abs),
			"version": opened.version,
		},
		"contentChanges": []map[string]any{{"text": opened.content}},
	}}); err != nil {
		return err
	}
	c.opened[abs] = opened
	return nil
}

func (c *Client) request(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID
	if err := writeMessage(c.stdin, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := readMessage(c.stdout)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			return nil, err
		}
		if envelope.ID == nil {
			continue
		}
		if *envelope.ID != id {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("gopls %s: %s", method, envelope.Error.Message)
		}
		return envelope.Result, nil
	}
}

func (c *Client) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeMessage(c.stdin, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func writeMessage(w io.Writer, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid gopls header %q", line)
		}
		if strings.EqualFold(name, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("missing gopls content length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(body), nil
}

func parseDefinition(raw json.RawMessage) (Location, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Location{}, false, nil
	}
	type lspLocation struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
	}
	type lspLocationLink struct {
		TargetURI   string `json:"targetUri"`
		TargetRange struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"targetRange"`
	}
	var one lspLocation
	if err := json.Unmarshal(raw, &one); err == nil && one.URI != "" {
		return Location{File: fromURI(one.URI), Line: one.Range.Start.Line + 1, Column: one.Range.Start.Character + 1}, true, nil
	}
	var many []lspLocation
	if err := json.Unmarshal(raw, &many); err == nil && len(many) > 0 && many[0].URI != "" {
		return Location{File: fromURI(many[0].URI), Line: many[0].Range.Start.Line + 1, Column: many[0].Range.Start.Character + 1}, true, nil
	}
	var links []lspLocationLink
	if err := json.Unmarshal(raw, &links); err == nil && len(links) > 0 && links[0].TargetURI != "" {
		return Location{File: fromURI(links[0].TargetURI), Line: links[0].TargetRange.Start.Line + 1, Column: links[0].TargetRange.Start.Character + 1}, true, nil
	}
	return Location{}, false, fmt.Errorf("unsupported gopls definition response")
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func fromURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return raw
	}
	return parsed.Path
}
