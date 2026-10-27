package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/store"
)

func TestAgentSkillReadExamplesMatchShippedCLI(t *testing.T) {
	skillPath := filepath.Join(repoRoot(t), ".agents", "skills", "sshdock-operations", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sqlite, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	fixtureApp := cli.App{Name: "my-app", Status: "created", NodeID: "local"}
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID: fixtureApp.Name, Name: fixtureApp.Name, NodeID: fixtureApp.NodeID,
		Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	fixtureDomain := cli.Domain{AppName: "my-app", DomainName: "my-app.example.com", ServiceName: "web", Port: 3000, HTTPS: true}
	if err := sqlite.AttachDomain(ctx, app.Domain{
		ID: fixtureDomain.DomainName, AppID: fixtureDomain.AppName,
		DomainName: fixtureDomain.DomainName, ServiceName: fixtureDomain.ServiceName,
		Port: fixtureDomain.Port, HTTPS: fixtureDomain.HTTPS, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{GitHost: "sshdock.example.com"})
	runner := cli.NewRunner(backend, "skill-contract-test")
	commands := agentSkillReadCommands(t, string(skill))
	surfaces := map[string]bool{}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			fields := strings.Fields(command)
			if len(fields) < 2 || fields[0] != "sshdock" {
				t.Fatalf("read example must be one full literal sshdock command: %q", command)
			}
			args := fields[1:]
			normalized, structured, err := cli.ParseOutputArgs(args)
			if err != nil || !structured || len(normalized) < 2 {
				t.Fatalf("read example is not a supported JSON query: structured=%t error=%v", structured, err)
			}
			surface := normalized[0] + "." + normalized[1]
			switch surface {
			case "apps.list", "apps.info", "apps.health", "releases.list", "deployments.list", "deployments.info", "domains.list", "domains.check", "events.list":
			default:
				t.Fatalf("read example needs an approved in-process fixture: %q", command)
			}
			if surface != "apps.list" && (len(normalized) < 3 || normalized[2] != fixtureApp.Name) {
				t.Fatalf("read example must query the documented app fixture: %q", command)
			}
			if surface == "deployments.info" && (len(normalized) != 4 || normalized[3] != "dep_example") {
				t.Fatalf("deployment example must use its documented placeholder: %q", command)
			}
			surfaces[surface] = true
			var stdout, stderr bytes.Buffer
			code := runner.Run(args, &stdout, &stderr)
			if code == 2 || stderr.Len() != 0 {
				t.Fatalf("unsupported example or invalid JSON channel: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var envelope struct {
				SchemaVersion string          `json:"schema_version"`
				Command       string          `json:"command"`
				Data          json.RawMessage `json:"data"`
				Error         *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			decoder := json.NewDecoder(&stdout)
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatalf("read example did not return JSON: %v", err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("read example must return exactly one JSON envelope: %v", err)
			}
			if envelope.SchemaVersion != "1" || envelope.Command != surface {
				t.Fatalf("unexpected schema/query identity: schema=%q command=%q", envelope.SchemaVersion, envelope.Command)
			}
			if surface == "deployments.info" {
				if code != 1 || envelope.Error == nil || envelope.Error.Code != "not_found" || envelope.Error.Message == "" || string(envelope.Data) != "null" {
					t.Fatalf("absent placeholder must be a typed query failure: exit=%d envelope=%+v", code, envelope)
				}
				return
			}
			if code != 0 || envelope.Error != nil {
				t.Fatalf("query failed: exit=%d envelope=%+v", code, envelope)
			}
			switch surface {
			case "apps.list":
				apps := decodeAgentSkillData[[]cli.App](t, envelope.Data)
				if len(apps) != 1 || apps[0] != fixtureApp {
					t.Fatalf("app list does not identify the fixture: %#v", apps)
				}
			case "apps.info":
				if got := decodeAgentSkillData[cli.App](t, envelope.Data); got != fixtureApp {
					t.Fatalf("app info = %#v, want %#v", got, fixtureApp)
				}
			case "apps.health":
				health := decodeAgentSkillData[cli.AppHealth](t, envelope.Data)
				if health.AppName != fixtureApp.Name || string(health.Status) != fixtureApp.Status || health.DomainCount != 1 || health.Health != "warn" || health.LatestDeploymentID != "" {
					t.Fatalf("successful health query must retain the undeployed app's warning state: %#v", health)
				}
			case "deployments.list", "releases.list", "events.list":
				if rows := decodeAgentSkillData[[]json.RawMessage](t, envelope.Data); rows == nil || len(rows) != 0 {
					t.Fatalf("empty history must be an array: %s", envelope.Data)
				}
			case "domains.list":
				if domains := decodeAgentSkillData[[]cli.Domain](t, envelope.Data); len(domains) != 1 || domains[0] != fixtureDomain {
					t.Fatalf("stored domain query = %#v, want %#v", domains, fixtureDomain)
				}
			case "domains.check":
				checks := decodeAgentSkillData[[]cli.DomainCheck](t, envelope.Data)
				if len(checks) != 1 || checks[0].DomainName != fixtureDomain.DomainName || checks[0].ServiceName != fixtureDomain.ServiceName || checks[0].Port != fixtureDomain.Port || checks[0].Status != "stored" {
					t.Fatalf("query must report stored intent when live routing is unobserved: %#v", checks)
				}
			}
		})
	}
	if len(surfaces) < 7 {
		t.Fatalf("skill covers %d distinct executable read surfaces, want at least seven", len(surfaces))
	}
	if app, err := backend.GetApp(fixtureApp.Name); err != nil || app != fixtureApp {
		t.Fatalf("read examples mutated the fixture app: %#v, error=%v", app, err)
	}
}

func agentSkillReadCommands(t *testing.T, skill string) []string {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(skill))
	var commands []string
	inside := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "```sshdock-read" {
			if inside {
				t.Fatal("nested sshdock-read fence")
			}
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if line == "```" {
			inside = false
		} else if line != "" {
			commands = append(commands, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if inside || len(commands) == 0 {
		t.Fatal("skill must contain closed sshdock-read fences with executable queries")
	}
	return commands
}

func decodeAgentSkillData[T any](t *testing.T, data json.RawMessage) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("query data does not match the shipped CLI type: %v\n%s", err, data)
	}
	return value
}
