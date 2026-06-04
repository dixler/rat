package fielddistance

import (
	"net/http"
	"time"

	"rat/internal/highlight"
	"rat/testdata/go/field_distance/other"
)

type SameFileType struct{}

type Demo struct {
	SameFile    SameFileType
	SamePackage SamePackageType
	SameProject other.ProjectType
	External    time.Time
	Builtin     int
	FuncBuiltin func(int) int
	FuncProject func() highlight.Span
}

func inlineDemo() {
	_ = struct {
		SameFile    SameFileType
		SamePackage SamePackageType
		SameProject other.ProjectType
		External    time.Time
		Builtin     int
		FuncBuiltin func(int) int
		FuncProject func() highlight.Span
	}{
		SameFile:    SameFileType{},
		SamePackage: SamePackageType{},
		SameProject: other.ProjectType{},
		External:    time.Time{},
		Builtin:     1,
		FuncBuiltin: func(v int) int { return v },
		FuncProject: func() highlight.Span { return highlight.Span{} },
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
		FuncProject: func() highlight.Span { return highlight.Span{} },
	}
}

func externalStructDemo() http.Server {
	return http.Server{
		Addr:        ":8080",
		Handler:     http.DefaultServeMux,
		ReadTimeout: time.Second,
	}
}
