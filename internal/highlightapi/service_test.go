package highlightapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandleRequestRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	payload, status := HandleRequest(strings.NewReader(`{"githubUrl":`), "./rat")

	require.Equal(t, 400, status)
	require.Equal(t, ResponseBody{Error: "invalid JSON payload"}, payload)
}

func TestHandleRequestRequiresGithubURL(t *testing.T) {
	t.Parallel()

	payload, status := HandleRequest(strings.NewReader(`{"githubUrl":"  "}`), "./rat")

	require.Equal(t, 400, status)
	require.Equal(t, ResponseBody{Error: "githubUrl is required"}, payload)
}
