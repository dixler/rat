package getrefs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUtilityHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644))

	loc := mkLoc(file, 2, 3)
	gotFile, gotLine, gotCol := locToFileLine(loc)
	require.Equal(t, file, gotFile)
	require.Equal(t, 2, gotLine)
	require.Equal(t, 3, gotCol)
	assert.Equal(t, "two", lineText(file, 2))
	assert.Equal(t, "", lineText(file, 99))

	dups := []Location{loc, loc, mkLoc(file, 3, 1)}
	require.Len(t, uniqLocs(dups), 2)

	toSort := []Location{mkLoc(file, 3, 1), mkLoc(file, 2, 9), mkLoc(file, 2, 2)}
	sortLocs(toSort)
	require.Equal(t, 2, locLine(toSort[0]))
	assert.Equal(t, 1, toSort[0].Range.Start.Character)
}

func TestReadMsgAndFiltering(t *testing.T) {
	t.Parallel()
	body := `{"ok":true}`
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	got, err := readMsg(bufio.NewReader(strings.NewReader(msg)))
	require.NoError(t, err)
	assert.Equal(t, body, string(got))

	_, err = readMsg(bufio.NewReader(strings.NewReader("\r\n{}")))
	require.Error(t, err)

	locs := []Location{mkLoc("/a.go", 1, 1), mkLoc("/b.go", 2, 1)}
	filtered := filterLocs(locs, func(f string) bool { return strings.HasSuffix(f, "a.go") })
	require.Len(t, filtered, 1)
	assert.True(t, containsLoc(filtered, mkLoc("/a.go", 1, 1)))
}

func mkLoc(file string, line, col int) Location {
	l := Location{URI: pathToURI(file)}
	l.Range.Start.Line = line - 1
	l.Range.Start.Character = col - 1
	l.Range.End = l.Range.Start
	return l
}
