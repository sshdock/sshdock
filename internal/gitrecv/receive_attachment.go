package gitrecv

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/app"
)

func (s *ReceivePackService) acceptedDeploymentAfterReceive(ctx context.Context, appID string, before []app.Deployment) (app.Deployment, bool, error) {
	beforeIDs := make(map[string]struct{}, len(before))
	for _, deployment := range before {
		beforeIDs[deployment.ID] = struct{}{}
	}
	after, err := s.store.ListDeploymentsByApp(ctx, appID)
	if err != nil {
		return app.Deployment{}, false, fmt.Errorf("list deployments after receive for %q: %w", appID, err)
	}
	events, err := s.store.ListEventsByApp(ctx, appID)
	if err != nil {
		return app.Deployment{}, false, fmt.Errorf("list events after receive for %q: %w", appID, err)
	}
	acceptedIDs := make(map[string]struct{}, len(events))
	for _, event := range events {
		if event.Type == "git.ref_accepted" {
			acceptedIDs[event.ID] = struct{}{}
		}
	}
	var accepted []app.Deployment
	for _, deployment := range after {
		if _, existed := beforeIDs[deployment.ID]; existed || deployment.Trigger != app.DeploymentTriggerPush {
			continue
		}
		if _, ok := acceptedIDs[EventID(deployment.ID, "git_ref_accepted")]; ok {
			accepted = append(accepted, deployment)
		}
	}
	if len(accepted) == 0 {
		return app.Deployment{}, false, nil
	}
	if len(accepted) > 1 {
		return app.Deployment{}, false, fmt.Errorf("receive created %d accepted deployments for app %q", len(accepted), appID)
	}
	return accepted[0], true, nil
}
