package goplsclient

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed gopls
var fs embed.FS

type Location struct {
	File   string
	Line   int
	Column int
}

type Client struct {
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stdoutPipe io.Closer
	cmd        *exec.Cmd

	mu        sync.Mutex
	closeOnce sync.Once
	nextID    int
	opened    map[string]openDocument
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
	goplsBin, err := binaryPath()
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
	c := &Client{stdin: stdin, stdout: bufio.NewReader(stdout), stdoutPipe: stdout, cmd: cmd, opened: map[string]openDocument{}}
	if err := c.initialize(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		if c.stdoutPipe != nil {
			_ = c.stdoutPipe.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		}
	})
	return nil
}

func (c *Client) SyncDocumentContent(file, content string) error {
	abs, err := filepath.Abs(file)
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
				"text":       content,
			},
		}}); err != nil {
			return err
		}
		c.opened[abs] = openDocument{version: 1, content: content}
		return nil
	}

	if opened.content == content {
		return nil
	}

	opened.version++
	opened.content = content
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

func (c *Client) HoverForPosition(pos token.Position) (string, error) {
	if pos.Filename == "" || pos.Line < 1 || pos.Column < 1 {
		return "", nil
	}
	result, err := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(pos.Filename)},
		"position":     map[string]any{"line": pos.Line - 1, "character": pos.Column - 1},
	})
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (c *Client) DefinitionForPosition(pos token.Position) (Location, bool, error) {
	if pos.Filename == "" || pos.Line < 1 || pos.Column < 1 {
		return Location{}, false, nil
	}
	result, err := c.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(pos.Filename)},
		"position":     map[string]any{"line": pos.Line - 1, "character": pos.Column - 1},
	})
	if err != nil {
		return Location{}, false, err
	}
	return parseDefinition(result)
}

func (c *Client) initialize() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, err = c.request("initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   fileURI(wd),
		"clientInfo": map[string]any{
			"name":    "rat",
			"version": "dev",
		},
		"capabilities": map[string]any{},
		"initializationOptions": map[string]any{
			"staticcheck":        false,
			"vulncheck":          "Off",
			"diagnosticsDelay":   "24h",
			"completeUnimported": false,
			"deepCompletion":     false,
			"codelenses": map[string]any{
				"generate":           false,
				"regenerate_cgo":     false,
				"tidy":               false,
				"upgrade_dependency": false,
				"vendor":             false,
			},
		},
	})
	if err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *Client) request(method string, params any) (json.RawMessage, error) {
	timedOut := make(chan struct{}, 1)
	timer := time.AfterFunc(5*time.Second, func() {
		timedOut <- struct{}{}
		_ = c.Close()
	})
	result, err := c.requestBlocking(method, params)
	if !timer.Stop() {
		select {
		case <-timedOut:
			return nil, fmt.Errorf("gopls %s timed out after 5s", method)
		default:
		}
	}
	return result, err
}

func (c *Client) requestBlocking(method string, params any) (json.RawMessage, error) {
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
			ID     json.RawMessage `json:"id"`
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
		if !matchesResponseID(envelope.ID, id) {
			if isServerRequest(envelope.ID, envelope.Method) {
				if err := writeMessage(c.stdin, map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "result": serverRequestResult(envelope.Method)}); err != nil {
					return nil, err
				}
			}
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

func isServerRequest(id json.RawMessage, method string) bool {
	return len(id) > 0 && method != ""
}

func serverRequestResult(method string) any {
	if method == "workspace/configuration" {
		return []any{}
	}
	return nil
}

func matchesResponseID(raw json.RawMessage, id int) bool {
	if len(raw) == 0 {
		return false
	}
	var numericID int
	if err := json.Unmarshal(raw, &numericID); err == nil {
		return numericID == id
	}
	var stringID string
	if err := json.Unmarshal(raw, &stringID); err == nil {
		return stringID == strconv.Itoa(id)
	}
	return false
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
			return nil, fmt.Errorf("invalid LSP header %q", line)
		}
		if strings.EqualFold(name, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("missing LSP content length")
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
	type lspPosition struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	}
	type lspRange struct {
		Start lspPosition `json:"start"`
	}
	type lspLocation struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	type lspLocationLink struct {
		TargetURI            string    `json:"targetUri"`
		TargetRange          lspRange  `json:"targetRange"`
		TargetSelectionRange *lspRange `json:"targetSelectionRange"`
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
		rangeStart := links[0].TargetRange.Start
		if links[0].TargetSelectionRange != nil {
			rangeStart = links[0].TargetSelectionRange.Start
		}
		return Location{File: fromURI(links[0].TargetURI), Line: rangeStart.Line + 1, Column: rangeStart.Character + 1}, true, nil
	}
	return Location{}, false, fmt.Errorf("unsupported LSP definition response")
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

func binaryPath() (string, error) {
	binary, err := fs.ReadFile("gopls")
	if err != nil {
		return "", err
	}
	if len(binary) == 0 {
		return "", fmt.Errorf("embedded gopls binary is empty")
	}

	name := fmt.Sprintf("gopls-%s-%s", runtime.GOOS, runtime.GOARCH)
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "rat")
	path := filepath.Join(dir, name)

	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0100 != 0 && info.Size() == int64(len(binary)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(dir, name+"-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}
