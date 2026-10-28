package cli

import (
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
	latest := releases[0]
	for _, release := range releases[1:] {
		if release.CreatedAt.After(latest.CreatedAt) || release.CreatedAt.Equal(latest.CreatedAt) && release.ID > latest.ID {
			latest = release
		}
	}
	return latest, true
}

func latestAppDeployment(deployments []appmodel.Deployment) (appmodel.Deployment, bool) {
	if len(deployments) == 0 {
		return appmodel.Deployment{}, false
	}
	latest := deployments[0]
	for _, deployment := range deployments[1:] {
		if deployment.StartedAt.After(latest.StartedAt) || deployment.StartedAt.Equal(latest.StartedAt) && deployment.ID > latest.ID {
			latest = deployment
		}
	}
	return latest, true
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
