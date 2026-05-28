package fielddistance

import (
	"net/http"
	"time"

	"rat/internal/display"
	"rat/testdata/rat/field_distance/other"
)

type SameFileType struct{}

type Demo struct {
	SameFile    SameFileType
	SamePackage SamePackageType
	SameProject other.ProjectType
	External    time.Time
	Builtin     int
	FuncBuiltin func(int) int
	FuncProject func() display.Span
}

func inlineDemo() {
	_ = struct {
		SameFile    SameFileType
		SamePackage SamePackageType
		SameProject other.ProjectType
		External    time.Time
		Builtin     int
		FuncBuiltin func(int) int
		FuncProject func() display.Span
	}{
		SameFile:    SameFileType{},
		SamePackage: SamePackageType{},
		SameProject: other.ProjectType{},
		External:    time.Time{},
		Builtin:     1,
		FuncBuiltin: func(v int) int { return v },
		FuncProject: func() display.Span { return display.Span{} },
	}
}

func namedDemo() Demo {
	return Demo{
		SameFile:    SameFileType{},
		SamePackage: SamePackageType{},
		SameProject: other.ProjectType{},
		External:    time.Time{},
		Builtin:     1,
		FuncBuiltin: func(v int) int { return v },
		FuncProject: func() display.Span { return display.Span{} },
	}
}

func externalStructDemo() http.Server {
	return http.Server{
		Addr:        ":8080",
		Handler:     http.DefaultServeMux,
		ReadTimeout: time.Second,
	}
}
