package deployment

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestExecutorStoresRedactedDeploymentOutputAcrossWrites(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	secret := "split-secret"
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         &splitOutputRunner{FakeRunner: &compose.FakeRunner{}, chunks: []string{"before split-", "secret after\n"}},
		ConfigResolver: staticDeploymentConfigResolver{values: map[string]string{"TOKEN": secret}},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, worktreePath string, _ string) error {
			writeExecutorComposeFixture(t, worktreePath)
			return nil
		}),
	})

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_output")

	// Then
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	log, err := sqlite.DeploymentLog(ctx, "my-app", "dep_output")
	if err != nil {
		t.Fatalf("DeploymentLog: %v", err)
	}
	if strings.Contains(log.Content, secret) || !strings.Contains(log.Content, "before <redacted> after") {
		t.Fatalf("deployment log = %q, want split secret redacted", log.Content)
	}
}

type staticDeploymentConfigResolver struct {
	values map[string]string
}

func (r staticDeploymentConfigResolver) ResolveEnv(context.Context, string) (map[string]string, error) {
	return nil, nil
}

func (r staticDeploymentConfigResolver) RedactionValues(context.Context, string) (map[string]string, error) {
	return r.values, nil
}

type splitOutputRunner struct {
	*compose.FakeRunner
	chunks []string
}

func (r *splitOutputRunner) DeployWithOutput(ctx context.Context, request compose.DeployRequest, stdout io.Writer, _ io.Writer) (compose.DeployResult, error) {
	for _, chunk := range r.chunks {
		if _, err := io.WriteString(stdout, chunk); err != nil {
			return compose.DeployResult{}, err
		}
	}
	return r.FakeRunner.Deploy(ctx, request)
}
