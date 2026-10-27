package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestBackendFactoryRunsOnlyAfterCommandValidation(t *testing.T) {
	for _, command := range []string{
		"apps check app", "apps create app", "apps list", "apps info app", "apps health app",
		"apps start app", "apps stop app", "apps restart app", "apps restart app web",
		"apps exec app web -- print --json", "apps run app web -- print --json",
		"apps remove app", "apps remove app --force --force", "apps redeploy app",
		"config set app TOKEN", "config import app", "config list app", "config keys app", "config get app TOKEN", "config unset app TOKEN",
		"domains list app", "domains check app", "domains detach app app.example.com", "domains attach app web app.example.com --port 3000",
		"events list app", "deployments info app dep_1", "deployments list app", "deployments logs -f app dep_1 -f -f",
		"logs app --tail=5 -f", "releases list app", "server domain set example.com",
		"ssh-keys list", "ssh-keys add laptop", "ssh-keys remove laptop",
	} {
		t.Run(command, func(t *testing.T) {
			calls := 0
			runner := NewRunnerWithBackendFactory(func() (Backend, error) {
				calls++
				return nil, errors.New("store unavailable")
			}, "test")
			var stdout, stderr bytes.Buffer
			if code := runner.Run(strings.Fields(command), &stdout, &stderr); code != 1 || calls != 1 || stdout.Len() != 0 || stderr.String() != "store unavailable\n" {
				t.Fatalf("exit=%d calls=%d stdout=%q stderr=%q", code, calls, &stdout, &stderr)
			}
		})
	}
	for _, command := range []string{
		"", "help", "-h", "--help", "help config", "apps --help", "version", "help missing", "help apps extra", "version extra", "unknown",
		"apps", "apps list extra", "apps create", "apps restart app web extra", "apps exec app web --", "apps run app web command", "apps remove", "apps remove app other --force",
		"config", "config get app", "config list app extra", "domains", "domains attach app web app.example.com --port invalid",
		"deployments logs", "deployments logs app dep_1 extra", "logs", "logs app --tail invalid", "events list", "releases list app extra",
		"server domain", "server domain set example.com extra", "ssh-keys remove", "ssh-keys list extra", "apps list extra --json",
		"check --json", "diagnostics --json",
	} {
		t.Run("no backend/"+command, func(t *testing.T) {
			calls := 0
			runner := NewRunnerWithBackendFactory(func() (Backend, error) {
				calls++
				return nil, errors.New("unexpected backend initialization")
			}, "test")
			runner.Run(strings.Fields(command), io.Discard, io.Discard)
			if calls != 0 {
				t.Fatalf("initialized backend %d times before a valid store command", calls)
			}
		})
	}
}

func TestBackendFactoryQueryErrorsUseJSONChannel(t *testing.T) {
	for _, command := range []string{
		"apps list", "apps info app", "apps health app", "apps check app", "releases list app", "deployments list app", "deployments info app dep_1", "domains list app", "domains check app", "events list app",
	} {
		t.Run(command, func(t *testing.T) {
			calls := 0
			runner := NewRunnerWithBackendFactory(func() (Backend, error) {
				calls++
				return nil, errors.New("store unavailable")
			}, "test")
			var stdout, stderr bytes.Buffer
			if code := runner.Run(append(strings.Fields(command), "--json"), &stdout, &stderr); code != 1 || calls != 1 || stderr.Len() != 0 {
				t.Fatalf("exit=%d calls=%d stdout=%s stderr=%s", code, calls, &stdout, &stderr)
			}
			envelope := decodeJSONEnvelope(t, stdout.Bytes())
			if envelope.Command != JSONCommandName(strings.Fields(command)) || envelope.Error == nil || envelope.Error.Code != "unavailable" || envelope.Error.Message != "store unavailable" || string(envelope.Data) != "null" {
				t.Fatalf("initialization error envelope = %s", &stdout)
			}
		})
	}
}

func TestBackendFactoryInitializesOnceAndRejectsMissingBackend(t *testing.T) {
	calls := 0
	runner := NewRunnerWithBackendFactory(func() (Backend, error) {
		calls++
		return NewMemoryBackend("server"), nil
	}, "test")
	for range 2 {
		if code := runner.Run([]string{"apps", "list"}, io.Discard, io.Discard); code != 0 {
			t.Fatalf("exit=%d", code)
		}
	}
	if calls != 1 {
		t.Fatalf("initialized backend %d times, want once", calls)
	}
	for _, runner := range []*Runner{NewRunner(nil, "test"), NewRunnerWithBackendFactory(func() (Backend, error) { return nil, nil }, "test")} {
		for _, args := range [][]string{{"apps", "list"}, {"apps", "list", "--json"}} {
			var stdout, stderr bytes.Buffer
			if code := runner.Run(args, &stdout, &stderr); code != 1 {
				t.Fatalf("missing backend exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		}
	}
}
