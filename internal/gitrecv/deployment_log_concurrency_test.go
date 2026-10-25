package gitrecv

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func TestDeploymentStdoutSurvivesConcurrentOperatorQueries(t *testing.T) {
	for _, prefill := range []int{0, 10 * 1024 * 1024} {
		name := "growing log"
		if prefill != 0 {
			name = "truncated log"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "sshdock.db")
			db := newHookTestStore(t, ctx, dbPath)
			now := time.Now()
			deployment := app.Deployment{ID: "dep_stdout", AppID: "my-app", CommitSHA: "abc123", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
			if err := db.QueueDeployment(ctx, deployment, ""); err != nil {
				t.Fatal(err)
			}
			if err := db.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, strings.Repeat("x", prefill), now); err != nil {
				t.Fatal(err)
			}
			writeHookComposeFixture(t, dir)
			secret := "stdout-secret-value"
			chunk := "stdout-chunk " + strings.Repeat("x", 1000) + secret + "\n"
			outputPath := filepath.Join(dir, "build-output")
			if err := os.WriteFile(outputPath, []byte(strings.Repeat(chunk, 4096)), 0o600); err != nil {
				t.Fatal(err)
			}

			started, stop, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			queries := 0
			go func() {
				select {
				case <-started:
				case <-stop:
					done <- nil
					return
				}
				for {
					select {
					case <-stop:
						done <- nil
						return
					default:
					}
					// Operator queries open a new store, including its migrations.
					query, err := store.OpenSQLite(ctx, dbPath)
					if err == nil {
						_, err = query.ListApps(ctx)
						closeErr := query.Close()
						if err == nil {
							err = closeErr
						}
					}
					if err != nil {
						done <- err
						return
					}
					queries++
					time.Sleep(time.Millisecond)
				}
			}()

			runner := compose.NewDockerRunner(concurrentStdoutExecutor{outputPath: outputPath, started: started})
			writer := compose.NewRedactingWriter(deploymentLogSink{ctx: ctx, store: db, appID: deployment.AppID, deploymentID: deployment.ID, now: time.Now}, map[string]string{"TOKEN": secret})
			_, deployErr := runner.DeployWithOutput(ctx, compose.DeployRequest{AppName: deployment.AppID, ProjectDir: dir, ComposePath: filepath.Join(dir, "compose.yml")}, writer, writer)
			close(stop)
			queryErr := <-done
			if deployErr != nil || queryErr != nil {
				t.Fatalf("deploy error = %v; concurrent query error = %v", deployErr, queryErr)
			}
			if queries == 0 {
				t.Fatal("no concurrent query completed")
			}
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}
			log, err := db.DeploymentLog(ctx, deployment.AppID, deployment.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(log.Content, secret) {
				t.Fatal("stored output contains the configured secret")
			}
			if prefill == 0 {
				if log.Truncated || strings.Count(log.Content, "stdout-chunk ") != 4096 || !strings.HasSuffix(log.Content, "stage: start services\n") {
					t.Fatal("streamed output was lost or truncated")
				}
			} else if !log.Truncated || len(log.Content) != 10*1024*1024 || strings.Count(log.Content, "[sshdock: deployment log truncated at 10 MiB]") != 1 {
				t.Fatal("capped deployment log changed while streaming more output")
			}
		})
	}
}

type concurrentStdoutExecutor struct {
	compose.LocalCommandExecutor
	outputPath string
	started    chan struct{}
}

func (e concurrentStdoutExecutor) Stream(ctx context.Context, command compose.Command, stdout, stderr io.Writer) error {
	switch command.Args[len(command.Args)-1] {
	case "json":
		_, err := io.WriteString(stdout, `{"services":{"web":{"image":"example/web:latest"}}}`)
		return err
	case "build":
		close(e.started)
		return e.LocalCommandExecutor.Stream(ctx, compose.Command{Name: "cat", Args: []string{e.outputPath}}, stdout, stderr)
	default:
		return nil
	}
}
