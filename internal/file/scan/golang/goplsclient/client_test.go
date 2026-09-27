package goplsclient

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentLifecycle(t *testing.T) {
	stream, err := os.CreateTemp(t.TempDir(), "messages")
	require.NoError(t, err)
	defer stream.Close()
	c := &Client{stdin: stream, opened: map[string]openDocument{}}
	path := filepath.Join(t.TempDir(), "sample.go")
	require.NoError(t, c.SyncDocumentContent(path, "package first"))
	require.NoError(t, c.SyncDocumentContent(path, "package second"))
	require.NoError(t, c.CloseDocument(path))
	require.Equal(t, openDocument{version: 2, refs: 1}, c.opened[path])
	require.NoError(t, c.CloseDocument(path))
	require.NoError(t, c.CloseDocument(path))
	require.Len(t, c.opened, 0)
	_, err = stream.Seek(0, io.SeekStart)
	require.NoError(t, err)
	reader := bufio.NewReader(stream)
	for _, want := range []string{
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"go","version":1,"text":"package first"}}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":2},"contentChanges":[{"text":"package second"}]}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":%q}}}`,
	} {
		raw, err := readMessage(reader)
		require.NoError(t, err)
		var actual, expected any
		require.NoError(t, json.Unmarshal(raw, &actual))
		require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(want, fileURI(path))), &expected))
		require.Equal(t, expected, actual)
	}
	_, err = reader.ReadByte()
	require.Equal(t, io.EOF, err)
}

func TestParseDefinitionLocationLinkUsesTargetSelectionRange(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"targetUri":"file:///tmp/example.go",
			"targetRange":{"start":{"line":9,"character":0},"end":{"line":12,"character":1}},
			"targetSelectionRange":{"start":{"line":10,"character":4},"end":{"line":10,"character":10}}
		}
	]`)

	loc, ok, err := parseDefinition(raw)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, Location{File: "/tmp/example.go", Line: 11, Column: 5}, loc)
}
