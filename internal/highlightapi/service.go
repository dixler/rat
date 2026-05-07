package highlightapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"rat/internal/ansihtml"
)

var githubBlobPath = regexp.MustCompile(`^/([^/]+)/([^/]+)/blob/([^/]+)/(.+)$`)

type RequestBody struct {
	GithubURL string `json:"githubUrl"`
}

type ResponseBody struct {
	HTML  string `json:"html,omitempty"`
	Error string `json:"error,omitempty"`
}

func Process(githubURL string, ratBinary string) (string, int, error) {
	rawURL, err := toRawGithubURL(githubURL)
	if err != nil {
		return "", http.StatusBadRequest, err
	}

	source, err := fetchSource(rawURL)
	if err != nil {
		return "", http.StatusBadGateway, err
	}

	tmpDir, err := os.MkdirTemp("", "rat-highlight-")
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("failed to create temp directory")
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, path.Base(rawURL.Path))
	if err := os.WriteFile(filePath, source, 0o600); err != nil {
		return "", http.StatusInternalServerError, errors.New("failed to write source file")
	}

	ansi, err := runRat(ratBinary, filePath)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}

	return ansihtml.Convert(ansi), http.StatusOK, nil
}

func Handler(ratBinary string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			writeJSON(w, http.StatusNoContent, ResponseBody{})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ResponseBody{Error: "method not allowed"})
			return
		}

		var payload RequestBody
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, ResponseBody{Error: "invalid JSON payload"})
			return
		}
		if strings.TrimSpace(payload.GithubURL) == "" {
			writeJSON(w, http.StatusBadRequest, ResponseBody{Error: "githubUrl is required"})
			return
		}

		html, code, err := Process(payload.GithubURL, ratBinary)
		if err != nil {
			writeJSON(w, code, ResponseBody{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ResponseBody{HTML: html})
	})
}

func writeJSON(w http.ResponseWriter, status int, payload ResponseBody) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "content-type")
	w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func toRawGithubURL(input string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil {
		return nil, errors.New("invalid GitHub URL")
	}
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return nil, errors.New("only github.com URLs are supported")
	}
	matches := githubBlobPath.FindStringSubmatch(u.EscapedPath())
	if len(matches) != 5 {
		return nil, errors.New("expected a GitHub blob URL")
	}
	return &url.URL{
		Scheme:  "https",
		Host:    "raw.githubusercontent.com",
		Path:    fmt.Sprintf("/%s/%s/%s/%s", matches[1], matches[2], matches[3], matches[4]),
		RawPath: fmt.Sprintf("/%s/%s/%s/%s", matches[1], matches[2], matches[3], matches[4]),
	}, nil
}

func fetchSource(rawURL *url.URL) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL.String(), nil)
	if err != nil {
		return nil, errors.New("failed to build upstream request")
	}
	req.Header.Set("User-Agent", "rat-highlight")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("failed to fetch source file")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

func runRat(ratBinary, filePath string) (string, error) {
	cmd := exec.Command(ratBinary, filePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rat failed: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
