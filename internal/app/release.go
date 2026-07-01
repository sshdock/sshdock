package app

import (
	"context"
	"sort"
)

type ReleaseHistory struct {
	Releases              []Release
	CurrentRelease        *Release
	LastSuccessfulRelease *Release
}

func (s *Service) ReleaseHistory(ctx context.Context, appID string) (ReleaseHistory, error) {
	releases, err := s.store.ListReleasesByApp(ctx, appID)
	if err != nil {
		return ReleaseHistory{}, err
	}

	sort.Slice(releases, func(i, j int) bool {
		return releases[i].CreatedAt.After(releases[j].CreatedAt)
	})

	history := ReleaseHistory{Releases: releases}
	for _, release := range releases {
		if release.Status == ReleaseStatusSucceeded {
			current := release
			history.CurrentRelease = &current
			lastSuccessful := release
			history.LastSuccessfulRelease = &lastSuccessful
			break
		}
	}

	return history, nil
}
