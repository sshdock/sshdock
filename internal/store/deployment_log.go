package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func (s *SQLiteStore) DeploymentLog(ctx context.Context, appID string, deploymentID string) (app.DeploymentLog, error) {
	row := s.db.QueryRowContext(ctx, `
		select deployment_id, app_id, content, truncated, created_at, updated_at
		from deployment_logs
		where app_id = ? and deployment_id = ?`, appID, deploymentID)
	log, err := scanDeploymentLog(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.DeploymentLog{}, notFound("deployment log", deploymentID)
	}
	return log, err
}

func (s *SQLiteStore) LatestDeploymentLog(ctx context.Context, appID string) (app.DeploymentLog, error) {
	row := s.db.QueryRowContext(ctx, `
		select deployment_id, app_id, content, truncated, created_at, updated_at
		from deployment_logs
		where app_id = ?
		order by created_at desc, deployment_id desc
		limit 1`, appID)
	log, err := scanDeploymentLog(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.DeploymentLog{}, notFound("deployment log", appID)
	}
	return log, err
}

func (s *SQLiteStore) AppendDeploymentLog(ctx context.Context, appID string, deploymentID string, output string, updatedAt time.Time) (returnErr error) {
	if output == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin deployment log append: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = tx.Rollback()
		}
	}()

	row := tx.QueryRowContext(ctx, `
		select deployment_id, app_id, content, truncated, created_at, updated_at
		from deployment_logs
		where app_id = ? and deployment_id = ?`, appID, deploymentID)
	log, err := scanDeploymentLog(row)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("deployment log", deploymentID)
	}
	if err != nil {
		return fmt.Errorf("load deployment log %q: %w", deploymentID, err)
	}

	content, truncated := appendDeploymentLogContent(log.Content, log.Truncated, output)
	if _, err := tx.ExecContext(ctx, `
		update deployment_logs
		set content = ?, truncated = ?, updated_at = ?
		where deployment_id = ?`, content, truncated, formatTime(updatedAt), deploymentID); err != nil {
		return fmt.Errorf("append deployment log %q: %w", deploymentID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deployment log append: %w", err)
	}
	return nil
}

type deploymentLogExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func createDeploymentLog(ctx context.Context, executor deploymentLogExecutor, deployment app.Deployment) error {
	if _, err := executor.ExecContext(ctx, `
		insert into deployment_logs (deployment_id, app_id, content, truncated, created_at, updated_at)
		values (?, ?, '', 0, ?, ?)`, deployment.ID, deployment.AppID, formatTime(deployment.StartedAt), formatTime(deployment.StartedAt)); err != nil {
		return fmt.Errorf("create deployment log: %w", err)
	}
	return nil
}

func retainRecentDeploymentLogs(ctx context.Context, executor deploymentLogExecutor, appID string) error {
	if _, err := executor.ExecContext(ctx, `
		delete from deployment_logs
		where deployment_id in (
			select deployment_id
			from deployment_logs
			where app_id = ?
			order by created_at desc, deployment_id desc
			limit -1 offset 20
		)`, appID); err != nil {
		return fmt.Errorf("retain deployment logs: %w", err)
	}
	return nil
}

func appendDeploymentLogContent(existing string, truncated bool, output string) (string, bool) {
	if truncated {
		return existing, true
	}
	maximumContentBytes := deploymentLogLimitBytes - len(deploymentLogTruncationMarker)
	if len(existing) >= maximumContentBytes {
		return existing[:maximumContentBytes] + deploymentLogTruncationMarker, true
	}
	remaining := maximumContentBytes - len(existing)
	if remaining >= len(output) {
		return existing + output, false
	}
	var builder strings.Builder
	builder.Grow(len(existing) + remaining + len(deploymentLogTruncationMarker))
	builder.WriteString(existing)
	builder.WriteString(output[:remaining])
	builder.WriteString(deploymentLogTruncationMarker)
	return builder.String(), true
}

func scanDeploymentLog(s scanner) (app.DeploymentLog, error) {
	var log app.DeploymentLog
	var truncated int
	var createdAt string
	var updatedAt string
	if err := s.Scan(&log.DeploymentID, &log.AppID, &log.Content, &truncated, &createdAt, &updatedAt); err != nil {
		return app.DeploymentLog{}, err
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return app.DeploymentLog{}, err
	}
	updated, err := parseTime(updatedAt)
	if err != nil {
		return app.DeploymentLog{}, err
	}
	log.CreatedAt = created
	log.UpdatedAt = updated
	log.Truncated = truncated != 0
	return log, nil
}
