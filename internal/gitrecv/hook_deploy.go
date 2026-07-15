package gitrecv

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

type postReceiveStore interface {
	CreateRelease(ctx context.Context, model app.Release) error
	GetReleaseByAppCommit(ctx context.Context, appID string, commitSHA string) (app.Release, error)
	CreateDeployment(ctx context.Context, model app.Deployment) error
	AttachDomain(ctx context.Context, model app.Domain) error
	ListDomains(ctx context.Context) ([]app.Domain, error)
	GetServerConfig(ctx context.Context) (store.ServerConfig, error)
	UpdateAppStatus(ctx context.Context, id string, status app.AppStatus, updatedAt time.Time) error
	UpdateReleaseStatus(ctx context.Context, id string, status app.ReleaseStatus, updatedAt time.Time) error
	MarkReleaseDeployingUnlessGood(ctx context.Context, id string, updatedAt time.Time) error
	MarkReleaseFailedUnlessGood(ctx context.Context, id string, updatedAt time.Time) error
	UpdateDeploymentStatus(ctx context.Context, id string, status app.DeploymentStatus, finishedAt time.Time, errorMessage string) error
	UpdateDeploymentFailure(ctx context.Context, model app.Deployment) error
	CreateEvent(ctx context.Context, model app.Event) error
}

type routeSyncer interface {
	SyncRoutes(ctx context.Context, routes []router.Route) error
}

type configResolver interface {
	ResolveAppConfig(ctx context.Context, appID string) (map[string]string, error)
}

type configRedactor interface {
	RedactionValues(ctx context.Context, appID string) (map[string]string, error)
}

type WorktreeCheckout interface {
	Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error
}

type WorktreeCheckoutFunc func(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error

func (f WorktreeCheckoutFunc) Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error {
	return f(ctx, repoPath, worktreePath, commitSHA)
}

type PostReceiveHandlerConfig struct {
	Store             postReceiveStore
	Runner            compose.Runner
	Router            routeSyncer
	ConfigResolver    configResolver
	Checkout          WorktreeCheckout
	Now               func() time.Time
	NewDeploymentID   func() (string, error)
	Output            io.Writer
	GitUpdateReported bool
}

type PostReceiveHandler struct {
	store             postReceiveStore
	runner            compose.Runner
	router            routeSyncer
	configResolver    configResolver
	checkout          WorktreeCheckout
	now               func() time.Time
	newDeploymentID   func() (string, error)
	output            io.Writer
	gitUpdateReported bool
}

// StatusOutputError reports that deployment state was persisted successfully,
// but the hook could not deliver its status lines to the Git client.
type StatusOutputError struct {
	Err error
}

func (e *StatusOutputError) Error() string { return e.Err.Error() }

func (e *StatusOutputError) Unwrap() error { return e.Err }

func NewPostReceiveHandler(config PostReceiveHandlerConfig) *PostReceiveHandler {
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	output := config.Output
	if output == nil {
		output = io.Discard
	}
	newDeploymentID := config.NewDeploymentID
	if newDeploymentID == nil {
		newDeploymentID = app.NewDeploymentID
	}

	return &PostReceiveHandler{
		store: config.Store, runner: config.Runner, router: config.Router,
		configResolver: config.ConfigResolver, checkout: config.Checkout, now: now,
		newDeploymentID: newDeploymentID, output: output,
		gitUpdateReported: config.GitUpdateReported,
	}
}

func (h *PostReceiveHandler) Handle(ctx context.Context, appName string, repoPath string, worktreePath string, input io.Reader) error {
	if h.store == nil {
		return fmt.Errorf("post-receive store is not configured")
	}
	if h.runner == nil {
		return fmt.Errorf("post-receive compose runner is not configured")
	}
	if h.checkout == nil {
		return fmt.Errorf("post-receive worktree checkout is not configured")
	}
	scanner := bufio.NewScanner(input)
	var outputErr error
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		event, err := ParsePostReceiveLine(appName, repoPath, line)
		if err != nil {
			return err
		}
		if !h.gitUpdateReported {
			if _, err := fmt.Fprintf(h.output, "git: remote main updated to %s\n", event.CommitSHA); err != nil {
				outputErr = errors.Join(outputErr, fmt.Errorf("write Git ref status: %w", err))
			}
		}
		if err := h.handleEvent(ctx, event, worktreePath); err != nil {
			return fmt.Errorf("current main %s: %w", event.CommitSHA, err)
		}
		if _, err := fmt.Fprintf(h.output, "deploy: current main %s succeeded\n", event.CommitSHA); err != nil {
			outputErr = errors.Join(outputErr, fmt.Errorf("write deploy status: %w", err))
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	if outputErr != nil {
		return &StatusOutputError{Err: outputErr}
	}
	return nil
}
