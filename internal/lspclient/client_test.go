package lspclient

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

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
