package app

import "context"

func (s *Service) resolveRedactionValues(ctx context.Context, appID string) (map[string]string, error) {
	if s.configResolver == nil {
		return nil, nil
	}
	if redactor, ok := s.configResolver.(configRedactor); ok {
		return redactor.RedactionValues(ctx, appID)
	}
	return s.configResolver.ResolveEnv(ctx, appID)
}
