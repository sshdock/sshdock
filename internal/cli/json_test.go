package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOutputArgs(t *testing.T) {
	for _, command := range []string{
		"apps list", "apps info app", "apps health app", "releases list app",
		"deployments list app", "deployments info app dep_1", "domains list app",
		"domains check app", "events list app", "diagnostics",
	} {
		t.Run(command, func(t *testing.T) {
			want := strings.Fields(command)
			args := append(append([]string(nil), want...), "--json")
			got, structured, err := ParseOutputArgs(args)
			if err != nil || !structured || !reflect.DeepEqual(got, want) {
				t.Fatalf("ParseOutputArgs(%v) = %v, %t, %v", args, got, structured, err)
			}
		})
	}
	for _, command := range []string{
		"apps list", "apps exec app web -- command --json", "apps run app web -- command --json --json",
	} {
		args := strings.Fields(command)
		got, structured, err := ParseOutputArgs(args)
		if err != nil || structured || !reflect.DeepEqual(got, args) {
			t.Fatalf("ParseOutputArgs(%v) changed plain command: %v, %t, %v", args, got, structured, err)
		}
	}
}

func TestJSONRejectsInvalidShapesBeforeBackendCalls(t *testing.T) {
	for _, command := range []string{
		"--json", "apps --json list", "apps list --json --json", "--json apps list",
		"apps info --help --json", "apps info --json", "apps list extra --json",
		"apps remove app --force --json", "config get app SECRET --json", "logs app --json",
		"deployments logs app --json", "deployments info app --json", "diagnostics extra --json",
		"unknown secret --json", "apps exec --json web -- command",
	} {
		t.Run(command, func(t *testing.T) {
			args := strings.Fields(command)
			_, structured, err := ParseOutputArgs(args)
			if err == nil || !structured {
				t.Fatalf("ParseOutputArgs(%v) = structured %t, error %v", args, structured, err)
			}
			var stdout, stderr bytes.Buffer
			if code := NewRunner(nil, "test").Run(args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			envelope := decodeJSONEnvelope(t, stdout.Bytes())
			if envelope.Error == nil || envelope.Error.Code != "invalid_arguments" || string(envelope.Data) != "null" || stderr.Len() != 0 {
				t.Fatalf("invalid command output = %s; stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestJSONReadCommands(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["app"] = App{Name: "app", Status: "failed", NodeID: "node"}
	now := time.Date(2026, 10, 23, 1, 2, 3, 4, time.UTC)
	backend.releases = []Release{{ID: "rel_1", AppName: "app", CommitSHA: "abc", Status: "rolled_back", CreatedAt: now}}
	backend.deployments = []Deployment{{ID: "dep_1", AppName: "app", ReleaseID: "rel_1", CommitSHA: "abc", Status: "pending", StartedAt: now}}
	backend.events = []Event{{ID: "evt_1", AppName: "app", Type: "deploy.queued", Message: "queued\nwith a newline", CreatedAt: now}}
	backend.domains = []Domain{{AppName: "app", DomainName: "app.example.com", ServiceName: "web", Port: 3000, HTTPS: true}}
	for _, test := range []struct {
		command string
		name    string
		field   string
		value   string
		list    bool
	}{
		{command: "apps list", name: "apps.list", field: "node_id", value: "node", list: true},
		{command: "apps info app", name: "apps.info", field: "name", value: "app"},
		{command: "apps health app", name: "apps.health", field: "health", value: "fail"},
		{command: "releases list app", name: "releases.list", field: "status", value: "rolled_back", list: true},
		{command: "deployments list app", name: "deployments.list", field: "commit_sha", value: "abc", list: true},
		{command: "deployments info app dep_1", name: "deployments.info", field: "status", value: "pending"},
		{command: "domains list app", name: "domains.list", field: "domain_name", value: "app.example.com", list: true},
		{command: "domains check app", name: "domains.check", field: "status", value: "stored", list: true},
		{command: "events list app", name: "events.list", field: "id", value: "evt_1", list: true},
	} {
		t.Run(test.command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append(strings.Fields(test.command), "--json")
			if code := NewRunner(backend, "test").Run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit = %d; stdout = %s; stderr = %q", code, stdout.String(), stderr.String())
			}
			envelope := decodeJSONEnvelope(t, stdout.Bytes())
			if envelope.Command != test.name || envelope.Error != nil {
				t.Fatalf("envelope = %s", stdout.String())
			}
			var item map[string]json.RawMessage
			if test.list {
				var items []map[string]json.RawMessage
				if err := json.Unmarshal(envelope.Data, &items); err != nil || len(items) != 1 {
					t.Fatalf("list = %s, error = %v", envelope.Data, err)
				}
				item = items[0]
			} else if err := json.Unmarshal(envelope.Data, &item); err != nil {
				t.Fatal(err)
			}
			if string(item[test.field]) != fmt.Sprintf("%q", test.value) {
				t.Fatalf("data[%s] = %s, want %q", test.field, item[test.field], test.value)
			}
			if test.name == "deployments.info" {
				if _, exists := item["finished_at"]; exists {
					t.Fatalf("unfinished deployment has finished_at: %s", envelope.Data)
				}
				if string(item["started_at"]) != `"2026-10-23T01:02:03.000000004Z"` {
					t.Fatalf("started_at = %s", item["started_at"])
				}
			}
		})
	}
}

func TestJSONListsAndNestedHealthCollectionsAreArrays(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["app"] = App{Name: "app"}
	for _, command := range []string{
		"apps list", "releases list app", "deployments list app", "domains list app", "domains check app", "events list app",
	} {
		t.Run(command, func(t *testing.T) {
			if command == "apps list" {
				backend = NewMemoryBackend("server")
			} else {
				backend.apps["app"] = App{Name: "app"}
			}
			var stdout bytes.Buffer
			if code := NewRunner(backend, "test").Run(append(strings.Fields(command), "--json"), &stdout, io.Discard); code != 0 {
				t.Fatalf("exit = %d", code)
			}
			if data := decodeJSONEnvelope(t, stdout.Bytes()).Data; string(data) != "[]" {
				t.Fatalf("empty list = %s, want []", data)
			}
		})
	}
	var stdout bytes.Buffer
	if code := NewRunner(&jsonHealthBackend{MemoryBackend: backend}, "test").Run([]string{"apps", "health", "app", "--json"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(decodeJSONEnvelope(t, stdout.Bytes()).Data, &data); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"checks", "services"} {
		if string(data[name]) != "[]" {
			t.Fatalf("health %s = %s, want []", name, data[name])
		}
	}
}

func TestJSONQueryErrorsAreTyped(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{err: fmt.Errorf("load app: %w", ErrNotFound), code: "not_found"},
		{err: errors.New("not found words in an unrelated runtime error"), code: "unavailable"},
		{err: fmt.Errorf("bootstrap: %w", JSONError("invalid_arguments", "bad arguments")), code: "invalid_arguments"},
	} {
		var stdout bytes.Buffer
		if err := WriteJSON(&stdout, "apps.list", []App{{Name: "must not appear"}}, test.err); err != nil {
			t.Fatal(err)
		}
		envelope := decodeJSONEnvelope(t, stdout.Bytes())
		if envelope.Error == nil || envelope.Error.Code != test.code || envelope.Error.Message != test.err.Error() || string(envelope.Data) != "null" {
			t.Fatalf("error envelope = %s", stdout.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := NewRunner(&jsonFailureBackend{MemoryBackend: NewMemoryBackend("server")}, "test").Run([]string{"apps", "list", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("backend error exit = %d", code)
	}
	if envelope := decodeJSONEnvelope(t, stdout.Bytes()); envelope.Error == nil || envelope.Error.Code != "unavailable" || stderr.Len() != 0 {
		t.Fatalf("backend error output = %s; stderr = %q", stdout.String(), stderr.String())
	}
	for _, args := range [][]string{{"apps", "list", "--json"}, {"apps", "list", "--json", "--json"}} {
		stderr.Reset()
		if code := NewRunner(NewMemoryBackend("server"), "test").Run(args, jsonFailureWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output closed") {
			t.Fatalf("writer failure exit = %d; stderr = %q", code, stderr.String())
		}
	}
}

func TestDeploymentInfoKeepsAppScopeAndExactAttemptStatus(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["app"] = App{Name: "app"}
	backend.apps["other"] = App{Name: "other"}
	backend.deployments = []Deployment{{ID: "dep_other", AppName: "other", Status: "succeeded"}}
	for _, status := range []string{"pending", "deploying", "succeeded", "failed"} {
		backend.deployments = append(backend.deployments, Deployment{ID: status, AppName: "app", Status: status})
		var stdout bytes.Buffer
		if code := NewRunner(backend, "test").Run([]string{"deployments", "info", "app", status, "--json"}, &stdout, io.Discard); code != 0 {
			t.Fatalf("%s query exit = %d", status, code)
		}
		if !bytes.Contains(decodeJSONEnvelope(t, stdout.Bytes()).Data, []byte(`"status":"`+status+`"`)) {
			t.Fatalf("status changed: %s", stdout.String())
		}
	}
	var stdout bytes.Buffer
	if code := NewRunner(backend, "test").Run([]string{"deployments", "info", "app", "dep_other", "--json"}, &stdout, io.Discard); code != 1 {
		t.Fatalf("cross-app query exit = %d", code)
	}
	if envelope := decodeJSONEnvelope(t, stdout.Bytes()); envelope.Error == nil || envelope.Error.Code != "not_found" {
		t.Fatalf("cross-app response = %s", stdout.String())
	}
	stdout.Reset()
	if code := NewRunner(backend, "test").Run([]string{"deployments", "info", "app", "failed"}, &stdout, io.Discard); code != 0 || !strings.Contains(stdout.String(), "id: failed\n") || !strings.Contains(stdout.String(), "status: failed\n") {
		t.Fatalf("human detail exit = %d, output = %q", code, stdout.String())
	}
}

func TestJSONCommandNameDoesNotIncludeArbitraryArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown-secret"}, {"apps", "secret-value"}, {"config", "get", "secret-value"}} {
		if strings.Contains(JSONCommandName(args), "secret") {
			t.Fatalf("command name leaks arguments: %v", args)
		}
	}
}

type jsonEnvelopeTest struct {
	SchemaVersion string          `json:"schema_version"`
	Command       string          `json:"command"`
	Data          json.RawMessage `json:"data"`
	Error         *outputError    `json:"error"`
}

func decodeJSONEnvelope(t *testing.T, output []byte) jsonEnvelopeTest {
	t.Helper()
	var envelope jsonEnvelopeTest
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("invalid JSON %q: %v", output, err)
	}
	if envelope.SchemaVersion != "1" || envelope.Command == "" || len(envelope.Data) == 0 {
		t.Fatalf("invalid envelope = %s", output)
	}
	return envelope
}

type jsonFailureBackend struct{ *MemoryBackend }

func (b *jsonFailureBackend) ListApps() ([]App, error) {
	return nil, errors.New("store unavailable")
}

type jsonFailureWriter struct{}

func (jsonFailureWriter) Write([]byte) (int, error) {
	return 0, errors.New("output closed")
}

type jsonHealthBackend struct{ *MemoryBackend }

func (b *jsonHealthBackend) AppHealth(string) (AppHealth, error) {
	return AppHealth{}, nil
}
