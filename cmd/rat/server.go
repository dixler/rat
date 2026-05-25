package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"rat/internal/display"
	"rat/internal/file"
)

type apiRequest struct { Path string `json:"path"` }
type apiSpan struct {
	Line int `json:"line"`; Start int `json:"start"`; End int `json:"end"`; Kind string `json:"kind"`
}
type apiResponse struct { Spans []apiSpan `json:"spans,omitempty"`; Error string `json:"error,omitempty"` }

func runServer(addr string) {
	http.HandleFunc("/spans", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "content-type")
		w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
		if r.Method == http.MethodOptions { w.WriteHeader(http.StatusNoContent); return }
		if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); _ = json.NewEncoder(w).Encode(apiResponse{Error: "method not allowed"}); return }
		var req apiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" { w.WriteHeader(http.StatusBadRequest); _ = json.NewEncoder(w).Encode(apiResponse{Error: "invalid request"}); return }
		spans, err := buildSpans(req.Path)
		if err != nil { w.WriteHeader(http.StatusBadRequest); _ = json.NewEncoder(w).Encode(apiResponse{Error: err.Error()}); return }
		_ = json.NewEncoder(w).Encode(apiResponse{Spans: spans})
	})
	_ = http.ListenAndServe(addr, nil)
}

func buildSpans(path string) ([]apiSpan, error) {
	f, err := file.Analyze(path)
	if err != nil { return nil, err }
	parsed := ParseFormats(f)
	out := make([]apiSpan, 0)
	for line, spans := range parsed.SourceSpans {
		for _, s := range spans {
			if k := spanKind(s.Style); k != "" { out = append(out, apiSpan{Line: line, Start: s.Start, End: s.End, Kind: k}) }
		}
	}
	sort.Slice(out, func(i, j int) bool { if out[i].Line != out[j].Line { return out[i].Line < out[j].Line }; if out[i].Start != out[j].Start { return out[i].Start < out[j].Start }; return out[i].End < out[j].End })
	return out, nil
}

func spanKind(st display.Style) string {
	s, ok := st.(display.BasicStyle)
	if !ok { return "" }
	x := string(s)
	switch {
	case strings.Contains(x, string(display.HotMagenta)): return "indirect"
	case strings.Contains(x, string(display.VibrantOrange)): return "parameter"
	case strings.Contains(x, string(display.Yellow)): return "variable"
	case strings.Contains(x, string(display.LightGreen)): return "type"
	case strings.Contains(x, string(display.Cyan)): return "samepkg"
	case strings.Contains(x, string(display.Blue)): return "project"
	case strings.Contains(x, string(display.Lavender)): return "external"
	case strings.Contains(x, string(display.Green)) || strings.Contains(x, string(display.MutedOrange)) || strings.Contains(x, string(display.Orange)): return "keyword"
	default: return ""
	}
}
