//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"testing"
)

func doSoftwareRecipeRequest(t *testing.T, client *http.Client, request *http.Request) (int, string) {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.URL, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read %s %s: %v", request.Method, request.URL, err)
	}
	return response.StatusCode, string(body)
}
