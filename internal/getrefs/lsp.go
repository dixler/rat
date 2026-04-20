package getrefs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

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
	Name     string   `json:"name"`
	Location Location `json:"location"`
}

type lspClient struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	nextID int
	mu     sync.Mutex
}

func newLSPClient(root string) (*lspClient, error) {
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
	return &lspClient{cmd: cmd, in: in, out: bufio.NewReader(out), nextID: 1}, nil
}

func (c *lspClient) Close() {
	_, _ = c.call("shutdown", map[string]any{})
	_ = c.notify("exit", map[string]any{})
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}

func (c *lspClient) Init(root string) error {
	_, err := c.call("initialize", map[string]any{
		"processId": os.Getpid(), "rootUri": pathToURI(root),
		"capabilities": map[string]any{"textDocument": map[string]any{}, "workspace": map[string]any{}},
	})
	if err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *lspClient) Find(q query) ([]Match, error) {
	res, err := c.call("workspace/symbol", map[string]any{"query": q.name})
	if err != nil {
		return nil, err
	}
	var all []symbolInfo
	if err := json.Unmarshal(res, &all); err != nil {
		return nil, err
	}
	var syms []symbolInfo
	for _, s := range all {
		if s.Name == q.name && q.inScope(locFile(s.Location)) {
			syms = append(syms, s)
		}
	}
	out := map[string]Match{}
	for _, s := range syms {
		def, refs, err := c.defAndRefs(s.Location)
		if err != nil || !q.inScope(locFile(def)) {
			continue
		}
		for _, g := range c.groupByDefinition(def, filterLocs(refs, q.inScope)) {
			if !q.inScope(locFile(g.Def)) {
				continue
			}
			k := locKey(g.Def)
			m := out[k]
			if m.Def.URI == "" {
				m.Def = g.Def
			}
			m.Refs = append(m.Refs, g.Refs...)
			out[k] = m
		}
	}
	matches := make([]Match, 0, len(out))
	for _, m := range out {
		m.Refs = uniqLocs(m.Refs)
		sort.Slice(m.Refs, func(i, j int) bool { return locKey(m.Refs[i]) < locKey(m.Refs[j]) })
		matches = append(matches, m)
	}
	sort.Slice(matches, func(i, j int) bool { return locKey(matches[i].Def) < locKey(matches[j].Def) })
	return matches, nil
}

func (c *lspClient) definitionAt(loc Location) Location {
	p := map[string]any{"textDocument": map[string]any{"uri": loc.URI}, "position": loc.Range.Start}
	defRaw, err := c.call("textDocument/definition", p)
	if err != nil || len(defRaw) == 0 || string(defRaw) == "null" {
		return loc
	}
	if defRaw[0] == '{' {
		var one Location
		if json.Unmarshal(defRaw, &one) == nil {
			return one
		}
		return loc
	}
	var defs []Location
	if json.Unmarshal(defRaw, &defs) == nil && len(defs) > 0 {
		return defs[0]
	}
	return loc
}

func (c *lspClient) defAndRefs(loc Location) (Location, []Location, error) {
	def := c.definitionAt(loc)
	refsRaw, err := c.call("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": def.URI}, "position": def.Range.Start,
		"context": map[string]any{"includeDeclaration": false},
	})
	if err != nil {
		return Location{}, nil, err
	}
	var refs []Location
	if len(refsRaw) > 0 && string(refsRaw) != "null" {
		if err := json.Unmarshal(refsRaw, &refs); err != nil {
			return Location{}, nil, err
		}
	}
	uniq := map[string]Location{}
	for _, r := range refs {
		if k := locKey(r); k != locKey(def) {
			uniq[k] = r
		}
	}
	refs = refs[:0]
	for _, r := range uniq {
		refs = append(refs, r)
	}
	return def, refs, nil
}

func (c *lspClient) groupByDefinition(def Location, refs []Location) []Match {
	groups := map[string]Match{}
	if len(refs) == 0 {
		groups[locKey(def)] = Match{Def: def}
	}
	for _, r := range refs {
		d := c.definitionAt(r)
		k := locKey(d)
		m := groups[k]
		if m.Def.URI == "" {
			m.Def = d
		}
		if !containsLoc(m.Refs, r) {
			m.Refs = append(m.Refs, r)
		}
		groups[k] = m
	}
	out := make([]Match, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return locKey(out[i].Def) < locKey(out[j].Def) })
	return out
}

func filterLocs(in []Location, keep func(string) bool) []Location {
	out := in[:0]
	for _, l := range in {
		if keep(locFile(l)) {
			out = append(out, l)
		}
	}
	return out
}

func containsLoc(locs []Location, target Location) bool {
	k := locKey(target)
	for _, l := range locs {
		if locKey(l) == k {
			return true
		}
	}
	return false
}

func (c *lspClient) notify(method string, params interface{}) error {
	b, err := json.Marshal(req{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (c *lspClient) call(method string, params interface{}) (json.RawMessage, error) {
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
		msg, err := readMsg(c.out)
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

func readMsg(r *bufio.Reader) ([]byte, error) {
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

func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
