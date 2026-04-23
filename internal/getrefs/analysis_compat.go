package getrefs

import (
	"notectl/internal/getrefs/astrefs"
)

type funcRef = astrefs.FuncRef
type namedLoc = astrefs.NamedLoc

type analysisClient struct{}

func (analysisClient) capturedRefsInFunction(ref Location) ([]*funcRef, []namedLoc) {
	return astrefs.CapturedRefsInFunction(ref)
}
