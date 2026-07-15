package cli

import (
	"sort"
	"strings"

	appmodel "github.com/sshdock/sshdock/internal/app"
)

func cliApp(model appmodel.App) App {
	return App{
		Name:   model.Name,
		Status: string(model.Status),
		NodeID: model.NodeID,
	}
}

func cliDomain(model appmodel.Domain) Domain {
	return Domain{
		AppName:     model.AppID,
		ServiceName: model.ServiceName,
		DomainName:  model.DomainName,
		Port:        model.Port,
		HTTPS:       model.HTTPS,
	}
}

func latestAppRelease(releases []appmodel.Release) (appmodel.Release, bool) {
	if len(releases) == 0 {
		return appmodel.Release{}, false
	}
	sorted := append([]appmodel.Release(nil), releases...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})
	return sorted[len(sorted)-1], true
}

func latestAppDeployment(deployments []appmodel.Deployment) (appmodel.Deployment, bool) {
	if len(deployments) == 0 {
		return appmodel.Deployment{}, false
	}
	sorted := append([]appmodel.Deployment(nil), deployments...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartedAt.Equal(sorted[j].StartedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})
	return sorted[len(sorted)-1], true
}

func isRunnableReleaseStatus(status appmodel.ReleaseStatus) bool {
	return status == appmodel.ReleaseStatusSucceeded || status == appmodel.ReleaseStatusRolledBack
}

func domainID(appName string, domainName string) string {
	return "dom_" + sanitizeIDPart(appName) + "_" + sanitizeIDPart(domainName)
}

func eventID(subjectID string, suffix string) string {
	return "evt_" + sanitizeIDPart(subjectID) + "_" + sanitizeIDPart(suffix)
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
