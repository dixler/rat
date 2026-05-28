package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"rat/internal/display"
	"rat/internal/highlight"
)

type apiRequest struct {
	Path string `json:"path"`
}
type apiResponse struct {
	Spans []display.Span `json:"spans,omitempty"`
	Error string         `json:"error,omitempty"`
}

func runServer(addr string) {
	http.HandleFunc("/spans", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "content-type")
		w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(apiResponse{Error: "method not allowed"})
			return
		}
		var req apiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(apiResponse{Error: "invalid request"})
			return
		}
		spans, err := buildSpans(req.Path)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(apiResponse{Error: err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(apiResponse{Spans: spans})
	})
	_ = http.ListenAndServe(addr, nil)
}

func buildSpans(path string) ([]display.Span, error) {
	program, err := highlight.Analyze(path)
	if err != nil {
		return nil, err
	}
	return apiSpans(program.SourceSpans), nil
}

func apiSpans(sourceSpans map[int][]display.Span) []display.Span {
	out := make([]display.Span, 0)
	for line, spans := range sourceSpans {
		for _, span := range spans {
			span.Line = line
			out = append(out, span)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].End < out[j].End
	})
	return out
}
