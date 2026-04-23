package getrefs

import (
	"notectl/internal/getrefs/astrefs"
	"notectl/internal/getrefs/refs"
)

type analyzer struct{}

func (analyzer) CapturedRefsInFunction(loc refs.Location) ([]*astrefs.FuncRef, []astrefs.NamedLoc) {
	return astrefs.CapturedRefsInFunction(loc)
}
