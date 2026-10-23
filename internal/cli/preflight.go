package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

type preflightBackend interface {
	Preflight(string) (compose.PreflightReport, error)
}

func (r *Runner) appPreflight(name string) (compose.PreflightReport, error) {
	backend, ok := r.backend.(preflightBackend)
	if !ok {
		return compose.PreflightReport{}, fmt.Errorf("app preflight is not configured")
	}
	return backend.Preflight(name)
}

func PrintPreflight(stdout io.Writer, report compose.PreflightReport) error {
	var output strings.Builder
	fmt.Fprintf(&output, "scope: %s\ncompose: %s\n", cliField(report.Scope), cliField(report.ComposePath))
	for _, check := range report.Checks {
		fmt.Fprintf(&output, "%s\t%s\tblocking=%t\t%s\n", cliField(check.Status), cliField(check.Name), check.Blocking, cliField(check.Detail))
		if check.Fix != "" {
			fmt.Fprintf(&output, "fix %s: %s\n", cliField(check.Name), cliField(check.Fix))
		}
	}
	if report.OK {
		output.WriteString("preflight: no confirmed blockers\n")
	} else {
		output.WriteString("preflight failed\n")
	}
	_, err := io.WriteString(stdout, output.String())
	return err
}
