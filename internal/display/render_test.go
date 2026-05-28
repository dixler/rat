package display

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlattenSpansMatchesRendererOverlapRules(t *testing.T) {
	spans := []Span{
		{Start: 0, End: 1, Style: HotMagenta},
		{Start: 0, End: 7, Style: Blue},
		{Start: 1, End: 2, Style: HotMagenta},
		{Start: 7, End: 13, Style: Purple},
		{Start: 7, End: 8, Style: HotMagenta},
		{Start: 13, End: 100, Style: Green},
	}

	got := FlattenSpans("promise.Errorf[struct{}]", spans)

	assert.Equal(t, []Span{
		{Start: 0, End: 1, Style: HotMagenta},
		{Start: 1, End: 2, Style: HotMagenta},
		{Start: 7, End: 13, Style: Purple},
		{Start: 13, End: 24, Style: Green},
	}, got)
}
