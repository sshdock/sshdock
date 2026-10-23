package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestPreflightOutputArgs(t *testing.T) {
	for _, command := range []string{"check", "check ./my-app", "apps check my-app"} {
		args := strings.Fields(command)
		normalized, structured, err := ParseOutputArgs(append(append([]string(nil), args...), "--json"))
		if err != nil || !structured || !reflect.DeepEqual(normalized, args) {
			t.Fatalf("parse %q = %v, %t, %v", command, normalized, structured, err)
		}
		want := "check"
		if args[0] == "apps" {
			want = "apps.check"
		}
		if got := JSONCommandName(normalized); got != want {
			t.Fatalf("command = %q, want %q", got, want)
		}
	}
	for _, command := range []string{"check first second --json", "check --json ./my-app", "apps check --json", "apps check app extra --json"} {
		if _, structured, err := ParseOutputArgs(strings.Fields(command)); err == nil || !structured {
			t.Fatalf("accepted unsupported preflight shape %q", command)
		}
	}
}

func TestAppsCheckReturnsBlockingReportWithoutQueryError(t *testing.T) {
	for _, structured := range []bool{false, true} {
		for _, ok := range []bool{false, true} {
			backend := &preflightOutputBackend{MemoryBackend: NewMemoryBackend("server"), report: compose.PreflightReport{
				OK: ok, Scope: "server", ComposePath: "/app/compose.yml",
				Checks: []compose.PreflightCheck{{Name: "compose", Status: "fail", Detail: "required DATABASE_URL is missing", Fix: "set app config", Blocking: !ok}},
			}}
			args := []string{"apps", "check", "my-app"}
			if structured {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := NewRunner(backend, "test").Run(args, &stdout, &stderr)
			wantCode := 1
			if ok {
				wantCode = 0
			}
			if code != wantCode || stderr.Len() != 0 || backend.name != "my-app" {
				t.Fatalf("structured=%t ok=%t: exit=%d stdout=%s stderr=%s", structured, ok, code, &stdout, &stderr)
			}
			if structured {
				envelope := decodeJSONEnvelope(t, stdout.Bytes())
				var report compose.PreflightReport
				if err := json.Unmarshal(envelope.Data, &report); err != nil || envelope.Command != "apps.check" || envelope.Error != nil || report.OK != ok || len(report.Checks) != 1 {
					t.Fatalf("preflight envelope = %s, err=%v", stdout.String(), err)
				}
			} else {
				for _, want := range []string{"scope: server", "compose: /app/compose.yml", "DATABASE_URL", "fix compose: set app config"} {
					if !strings.Contains(stdout.String(), want) {
						t.Fatalf("human output missing %q: %s", want, &stdout)
					}
				}
				if ok && !strings.Contains(stdout.String(), "preflight: no confirmed blockers") {
					t.Fatalf("successful report overstates readiness: %s", &stdout)
				}
			}
		}
	}
}

func TestAppsCheckQueryErrorsAndEmptyChecks(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{err: appNotFoundError("missing"), code: "not_found"},
		{err: errors.New("runtime query unavailable"), code: "unavailable"},
		{code: ""},
	} {
		backend := &preflightOutputBackend{MemoryBackend: NewMemoryBackend("server"), report: compose.PreflightReport{OK: true, Scope: "server"}, err: test.err}
		var stdout bytes.Buffer
		code := NewRunner(backend, "test").Run([]string{"apps", "check", "app", "--json"}, &stdout, io.Discard)
		envelope := decodeJSONEnvelope(t, stdout.Bytes())
		if test.err != nil {
			if code != 1 || envelope.Error == nil || envelope.Error.Code != test.code || string(envelope.Data) != "null" {
				t.Fatalf("error exit=%d envelope=%s", code, &stdout)
			}
		} else if code != 0 || envelope.Error != nil || !bytes.Contains(envelope.Data, []byte(`"checks":[]`)) {
			t.Fatalf("empty report exit=%d envelope=%s", code, &stdout)
		}
	}
	var stdout bytes.Buffer
	if code := NewRunner(NewMemoryBackend("server"), "test").Run([]string{"apps", "check", "app", "--json"}, &stdout, io.Discard); code != 1 {
		t.Fatalf("unsupported backend exit=%d", code)
	}
	if envelope := decodeJSONEnvelope(t, stdout.Bytes()); envelope.Error == nil || envelope.Error.Code != "unavailable" {
		t.Fatalf("unsupported backend output=%s", &stdout)
	}
}

func TestPrintPreflightSanitizesHumanRowsAndReportsWriterFailure(t *testing.T) {
	report := compose.PreflightReport{Scope: "local", Checks: []compose.PreflightCheck{{Name: "compose\nfile", Status: "fail", Detail: "bad\tline\x1b", Fix: "replace\nfile", Blocking: true}}}
	var stdout bytes.Buffer
	if err := PrintPreflight(&stdout, report); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "\x1b") || strings.Contains(stdout.String(), "bad\tline") || !strings.Contains(stdout.String(), "fix compose file: replace file") {
		t.Fatalf("unsafe human rows: %q", stdout.String())
	}
	if err := PrintPreflight(jsonFailureWriter{}, report); err == nil {
		t.Fatal("PrintPreflight ignored writer failure")
	}
}

type preflightOutputBackend struct {
	*MemoryBackend
	report compose.PreflightReport
	err    error
	name   string
}

func (b *preflightOutputBackend) Preflight(name string) (compose.PreflightReport, error) {
	b.name = name
	return b.report, b.err
}
