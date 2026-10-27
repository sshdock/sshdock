package deployment

import (
	"context"
	"fmt"
	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
	"github.com/sshdock/sshdock/internal/router"
	"time"
)

type autoRouteRequest struct {
	AppName      string
	Result       compose.DeployResult
	DeploymentID string
	CreatedAt    time.Time
}

func (e *Executor) autoRoute(ctx context.Context, request autoRouteRequest) error {
	if e.routing == nil {
		return fmt.Errorf("initial routing is not configured")
	}
	var target *router.Route
	if request.Result.RouteFound {
		target = &router.Route{ServiceName: request.Result.RouteTarget.ServiceName, Port: request.Result.RouteTarget.Port}
	}
	reason, err := e.routing.EnsureInitial(ctx, router.InitialRouteRequest{AppID: request.AppName, DeploymentID: request.DeploymentID, Target: target, NoTargetReason: request.Result.RouteReason})
	if err != nil {
		return err
	}
	if reason != "" {
		return e.recordAutoRouteSkipped(ctx, request.AppName, request.DeploymentID, reason, request.CreatedAt)
	}
	return nil
}

func (e *Executor) recordAutoRouteSkipped(ctx context.Context, appName string, deploymentID string, reason string, createdAt time.Time) error {
	message := deployfailure.Message("route inference", reason, "containers deployed, routes unchanged", "attach manually with sshdock domains attach", "sudo sshdock domains attach "+appName+" <service> <domain> --port <port>")
	if err := e.store.CreateEvent(ctx, app.Event{ID: eventID(deploymentID, "route_auto_skipped"), AppID: appName, Type: "route.auto_skipped", Message: message, CreatedAt: createdAt.Add(time.Second)}); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(e.output, message); err != nil {
		return nil
	}
	return nil
}
