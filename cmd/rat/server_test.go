package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rat/internal/display"
)

func TestAPIResponseUsesDisplaySpans(t *testing.T) {
	resp := apiResponse{Spans: []display.Span{
		{Line: 1, Start: 0, End: 1, Style: display.HotMagenta},
		{Line: 1, Start: 1, End: 8, Style: display.Blue},
	}}

	got, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.Equal(t, `{"spans":[{"line":1,"start":0,"end":1,"style":"\u001b[38;5;198m"},{"line":1,"start":1,"end":8,"style":"\u001b[94m"}]}`, string(got))
}
