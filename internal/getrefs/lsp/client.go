package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

	"notectl/internal/getrefs/refs"
)

type Query struct {
	Name    string
	InScope func(string) bool
}

type req struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
}

type resp struct {
	ID     int             `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Message string `json:"message"`
}

type symbolInfo struct {
	Name     string        `json:"name"`
	Location refs.Location `json:"location"`
}

type Client struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	nextID int
	mu     sync.Mutex
}

func New(root string) (*Client, error) {
	cmd := exec.Command("gopls", "serve")
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, errors.New("failed to start gopls; install it with `go install golang.org/x/tools/gopls@latest`")
	}
	return &Client{cmd: cmd, in: in, out: bufio.NewReader(out), nextID: 1}, nil
}

func (c *Client) Close() {
	_, _ = c.call("shutdown", map[string]any{})
	_ = c.notify("exit", map[string]any{})
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}

func (c *Client) Init(root string) error {
	_, err := c.call("initialize", map[string]any{"processId": os.Getpid(), "rootUri": refs.PathToURI(root), "capabilities": map[string]any{"textDocument": map[string]any{}, "workspace": map[string]any{}}})
	if err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *Client) Find(q Query) ([]refs.Match, error) {
	res, err := c.call("workspace/symbol", map[string]any{"query": q.Name})
	if err != nil {
		return nil, err
	}
	var all []symbolInfo
	if err := json.Unmarshal(res, &all); err != nil {
		return nil, err
	}
	var syms []symbolInfo
	for _, s := range all {
		if s.Name == q.Name && q.InScope(refs.File(s.Location)) {
			syms = append(syms, s)
		}
	}
	out := map[string]refs.Match{}
	for _, s := range syms {
		def, rs, err := c.DefAndRefs(s.Location)
		if err != nil || !q.InScope(refs.File(def)) {
			continue
		}
		for _, g := range c.groupByDefinition(def, filterLocs(rs, q.InScope)) {
			if !q.InScope(refs.File(g.Def)) {
				continue
			}
			k := refs.Key(g.Def)
			m := out[k]
			if m.Def.URI == "" {
				m.Def = g.Def
			}
			m.Refs = append(m.Refs, g.Refs...)
			out[k] = m
		}
	}
	matches := make([]refs.Match, 0, len(out))
	for _, m := range out {
		m.Refs = refs.UniqLocs(m.Refs)
		sort.Slice(m.Refs, func(i, j int) bool { return refs.Key(m.Refs[i]) < refs.Key(m.Refs[j]) })
		matches = append(matches, m)
	}
	sort.Slice(matches, func(i, j int) bool { return refs.Key(matches[i].Def) < refs.Key(matches[j].Def) })
	return matches, nil
}

func (c *Client) DefinitionAt(loc refs.Location) refs.Location {
	p := map[string]any{"textDocument": map[string]any{"uri": loc.URI}, "position": loc.Range.Start}
	defRaw, err := c.call("textDocument/definition", p)
	if err != nil || len(defRaw) == 0 || string(defRaw) == "null" {
		return loc
	}
	if defRaw[0] == '{' {
		var one refs.Location
		if json.Unmarshal(defRaw, &one) == nil {
			return one
		}
		return loc
	}
	var defs []refs.Location
	if json.Unmarshal(defRaw, &defs) == nil && len(defs) > 0 {
		return defs[0]
	}
	return loc
}

func (c *Client) DefAndRefs(loc refs.Location) (refs.Location, []refs.Location, error) {
	def := c.DefinitionAt(loc)
	raw, err := c.call("textDocument/references", map[string]any{"textDocument": map[string]any{"uri": def.URI}, "position": def.Range.Start, "context": map[string]any{"includeDeclaration": false}})
	if err != nil {
		return refs.Location{}, nil, err
	}
	var rs []refs.Location
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rs); err != nil {
			return refs.Location{}, nil, err
		}
	}
	uniq := map[string]refs.Location{}
	for _, r := range rs {
		if k := refs.Key(r); k != refs.Key(def) {
			uniq[k] = r
		}
	}
	rs = rs[:0]
	for _, r := range uniq {
		rs = append(rs, r)
	}
	return def, rs, nil
}

func (c *Client) groupByDefinition(def refs.Location, rs []refs.Location) []refs.Match {
	groups := map[string]refs.Match{}
	if len(rs) == 0 {
		groups[refs.Key(def)] = refs.Match{Def: def}
	}
	for _, r := range rs {
		d := c.DefinitionAt(r)
		k := refs.Key(d)
		m := groups[k]
		if m.Def.URI == "" {
			m.Def = d
		}
		if !containsLoc(m.Refs, r) {
			m.Refs = append(m.Refs, r)
		}
		groups[k] = m
	}
	out := make([]refs.Match, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return refs.Key(out[i].Def) < refs.Key(out[j].Def) })
	return out
}

func filterLocs(in []refs.Location, keep func(string) bool) []refs.Location {
	out := in[:0]
	for _, l := range in {
		if keep(refs.File(l)) {
			out = append(out, l)
		}
	}
	return out
}

func containsLoc(locs []refs.Location, t refs.Location) bool {
	k := refs.Key(t)
	for _, l := range locs {
		if refs.Key(l) == k {
			return true
		}
	}
	return false
}

func (c *Client) notify(method string, params interface{}) error {
	b, err := json.Marshal(req{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (c *Client) call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	b, err := json.Marshal(req{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b); err != nil {
		return nil, err
	}
	for {
		msg, err := ReadMsg(c.out)
		if err != nil {
			return nil, err
		}
		var r resp
		if json.Unmarshal(msg, &r) != nil || r.ID == 0 || r.ID != id {
			continue
		}
		if r.Error != nil {
			return nil, errors.New(r.Error.Message)
		}
		return r.Result, nil
	}
}

func ReadMsg(r *bufio.Reader) ([]byte, error) {
	headers := map[string]string{}
	for {
		ln, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		ln = strings.TrimRight(ln, "\r\n")
		if ln == "" {
			break
		}
		parts := strings.SplitN(ln, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(strings.ToLower(parts[0]))] = strings.TrimSpace(parts[1])
		}
	}
	n, err := strconv.Atoi(headers["content-length"])
	if err != nil || n <= 0 {
		return nil, errors.New("invalid lsp content-length")
	}
	buf := make([]byte, n)
	_, err = io.ReadFull(r, buf)
	return buf, err
}
