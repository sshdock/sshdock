package gitrecv

import (
	"strings"

	"github.com/sshdock/sshdock/internal/app"
)

func ReleaseID(appName string, commitSHA string) string {
	return app.ReleaseID(appName, commitSHA)
}

func EventID(deploymentID string, suffix string) string {
	return "evt_" + deploymentID + "_" + suffix
}

func domainID(appName string, domainName string) string {
	return "dom_" + sanitizeIDPart(appName) + "_" + sanitizeIDPart(domainName)
}

func sanitizeIDPart(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	return strings.Trim(builder.String(), "_")
}
