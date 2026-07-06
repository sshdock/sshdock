package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestServiceLogsRequestsRunnerLogs(t *testing.T) {
	ctx := context.Background()
	runner := &compose.FakeRunner{LogOutput: "web log\n"}
	service := NewService(newFakeServiceStore(), WithLogRunner(runner))

	logs, err := service.Logs(ctx, "my-app", "web", 50)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if logs != "web log\n" {
		t.Fatalf("logs = %q", logs)
	}

	want := compose.LogsRequest{AppName: "my-app", ServiceName: "web", Lines: 50}
	if len(runner.LogsRequests) != 1 || !reflect.DeepEqual(runner.LogsRequests[0], want) {
		t.Fatalf("LogsRequests = %#v, want [%#v]", runner.LogsRequests, want)
	}
}

func TestServiceLogsReturnsRunnerError(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("logs failed")
	runner := &compose.FakeRunner{LogsErr: failure}
	service := NewService(newFakeServiceStore(), WithLogRunner(runner))

	_, err := service.Logs(ctx, "my-app", "web", 50)
	if !errors.Is(err, failure) {
		t.Fatalf("Logs error = %v, want %v", err, failure)
	}
}
