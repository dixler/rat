package getrefs

import (
	"notectl/internal/getrefs/refs"
	"notectl/internal/getrefs/view"
)

type defResolver interface{ definitionAt(Location) Location }

type defAdapter struct{ d defResolver }

func (a defAdapter) DefinitionAt(loc refs.Location) refs.Location { return a.d.definitionAt(loc) }

func Render(r defResolver, repoRoot, name string, matches []refs.Match) error {
	return view.Render(defAdapter{d: r}, analyzer{}, repoRoot, name, matches)
}
