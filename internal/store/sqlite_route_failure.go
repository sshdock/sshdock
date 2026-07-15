package store

import (
	"context"
	"fmt"
)

func (s *SQLiteStore) UpsertRouteApplyFailure(ctx context.Context, failure RouteApplyFailure) error {
	_, err := s.db.ExecContext(ctx, `
		insert into route_apply_failures (
			app_id, service_name, domain_name, port, https, operation, detail, updated_at
		) values (?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(app_id, domain_name) do update set
			service_name = excluded.service_name,
			port = excluded.port,
			https = excluded.https,
			operation = excluded.operation,
			detail = excluded.detail,
			updated_at = excluded.updated_at`,
		failure.AppID,
		failure.ServiceName,
		failure.DomainName,
		failure.Port,
		failure.HTTPS,
		string(failure.Operation),
		failure.Detail,
		formatTime(failure.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert route apply failure for %q: %w", failure.DomainName, err)
	}
	return nil
}

func (s *SQLiteStore) ListRouteApplyFailuresByApp(ctx context.Context, appID string) ([]RouteApplyFailure, error) {
	rows, err := s.db.QueryContext(ctx, `
		select app_id, service_name, domain_name, port, https, operation, detail, updated_at
		from route_apply_failures
		where app_id = ?
		order by updated_at, domain_name`, appID)
	if err != nil {
		return nil, fmt.Errorf("list route apply failures for app %q: %w", appID, err)
	}
	defer rows.Close()

	failures := make([]RouteApplyFailure, 0)
	for rows.Next() {
		var failure RouteApplyFailure
		var operation string
		var updatedAt string
		if err := rows.Scan(
			&failure.AppID,
			&failure.ServiceName,
			&failure.DomainName,
			&failure.Port,
			&failure.HTTPS,
			&operation,
			&failure.Detail,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan route apply failure: %w", err)
		}
		failure.Operation = RouteApplyOperation(operation)
		parsed, err := parseTime(updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse route apply failure time: %w", err)
		}
		failure.UpdatedAt = parsed
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list route apply failure rows: %w", err)
	}
	return failures, nil
}

func (s *SQLiteStore) ClearRouteApplyFailures(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `delete from route_apply_failures`); err != nil {
		return fmt.Errorf("clear route apply failures: %w", err)
	}
	return nil
}
