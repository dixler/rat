package getrefs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithFakeGopls(t *testing.T) {
	repo := t.TempDir()
	file := filepath.Join(repo, "main.go")
	src := "package main\n\nfunc f() {\n\ttarget := 1\n\t_ = target\n}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
	cmd := exec.Command("git", "init")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gopls")
	def := fmt.Sprintf(`{"uri":%q,"range":{"start":{"line":3,"character":1},"end":{"line":3,"character":1}}}`, pathToURI(file))
	ref := fmt.Sprintf(`{"uri":%q,"range":{"start":{"line":4,"character":5},"end":{"line":4,"character":5}}}`, pathToURI(file))
	py := "#!/usr/bin/env python3\n" +
		"import json,sys\n" +
		"def readmsg():\n" +
		"  h={}\n" +
		"  while True:\n" +
		"    ln=sys.stdin.buffer.readline()\n" +
		"    if not ln: return None\n" +
		"    ln=ln.decode().strip('\\r\\n')\n" +
		"    if ln=='': break\n" +
		"    k,v=ln.split(':',1); h[k.lower().strip()]=v.strip()\n" +
		"  n=int(h.get('content-length','0'))\n" +
		"  return json.loads(sys.stdin.buffer.read(n).decode())\n" +
		"def send(i,res):\n" +
		"  b=json.dumps({'jsonrpc':'2.0','id':i,'result':res}).encode()\n" +
		"  sys.stdout.buffer.write(f'Content-Length: {len(b)}\\r\\n\\r\\n'.encode()+b); sys.stdout.flush()\n" +
		"while True:\n" +
		"  m=readmsg()\n" +
		"  if m is None: break\n" +
		"  if 'id' not in m: continue\n" +
		"  meth=m.get('method')\n" +
		"  if meth=='initialize': send(m['id'], {})\n" +
		"  elif meth=='workspace/symbol': send(m['id'], [{'name':'target','location':" + def + "}])\n" +
		"  elif meth=='textDocument/definition': send(m['id'], " + def + ")\n" +
		"  elif meth=='textDocument/references': send(m['id'], [" + ref + "])\n" +
		"  elif meth=='shutdown': send(m['id'], {})\n" +
		"  else: send(m['id'], None)\n"
	require.NoError(t, os.WriteFile(script, []byte(py), 0o755))

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldWD, _ := os.Getwd()
	require.NoError(t, os.Chdir(repo))
	defer os.Chdir(oldWD)

	runOut := captureStdout(func() { require.NoError(t, Run("main.go:target")) })
	clean := stripANSI(runOut)
	assert.Contains(t, clean, "Definition")
	assert.Contains(t, clean, "Ref 1")
}

func TestRenderNoMatches(t *testing.T) {
	out := captureStdout(func() { require.NoError(t, Render(fakeResolver{}, "/repo", "x", nil)) })
	assert.Contains(t, out, `no identifier matches for "x"`)
}

type fakeResolver struct{}

func (fakeResolver) definitionAt(l Location) Location { return l }

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	return b.String()
}

func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(s, "")
}
