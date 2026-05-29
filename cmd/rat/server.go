package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"rat/internal/display"
	"rat/internal/highlight"
)

type apiRequest struct {
	Path string `json:"path"`
}
type apiResponse struct {
	Spans map[int][]display.Span `json:"spans,omitempty"`
	Error string                 `json:"error,omitempty"`
}

func runServer(addr string) {
	http.HandleFunc("/spans", handleFunc)
	_ = http.ListenAndServe(addr, nil)
}

func handleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "content-type")
	w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
	status, response := handle(r)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func handle(r *http.Request) (int, apiResponse) {
	switch r.Method {
	case http.MethodOptions:
		return http.StatusNoContent, apiResponse{}
	case http.MethodPost:
		var req apiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
			return http.StatusBadRequest, apiResponse{Error: "invalid request"}
		}
		program, err := highlight.Analyze(req.Path)
		if err != nil {
			return http.StatusBadRequest, apiResponse{Error: err.Error()}
		}
		return http.StatusOK, apiResponse{Spans: program.SourceSpans}
	default:
		return http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"}
	}
}
