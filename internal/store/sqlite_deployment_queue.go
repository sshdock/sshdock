package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func (s *SQLiteStore) FindDeploymentByAppCommit(ctx context.Context, appID string, commitSHA string) (app.Deployment, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, app_id, release_id, commit_sha, trigger, status, started_at, finished_at,
		       failure_stage, failure_detail, retry_guidance, error_message
		from deployments
		where app_id = ? and commit_sha = ?
		order by started_at desc, id desc
		limit 1`,
		appID,
		commitSHA,
	)
	deployment, err := scanDeployment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Deployment{}, notFound("deployment for app commit", appID+"/"+commitSHA)
	}
	if err != nil {
		return app.Deployment{}, err
	}
	return deployment, nil
}

func (s *SQLiteStore) ListDeploymentsByStatus(ctx context.Context, status app.DeploymentStatus) ([]app.Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, app_id, release_id, commit_sha, trigger, status, started_at, finished_at,
		       failure_stage, failure_detail, retry_guidance, error_message
		from deployments
		where status = ?
		order by started_at, id`, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []app.Deployment
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deployments, nil
}

func (s *SQLiteStore) ClaimNextPendingDeployment(ctx context.Context) (deployment app.Deployment, found bool, returnErr error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.Deployment{}, false, fmt.Errorf("begin deployment claim transaction: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = tx.Rollback()
		}
	}()

	row := tx.QueryRowContext(ctx, `
		select id, app_id, release_id, commit_sha, trigger, status, started_at, finished_at,
		       failure_stage, failure_detail, retry_guidance, error_message
		from deployments
		where status = ?
		  and exists (
			select 1 from events
			where id = 'evt_' || deployments.id || '_queued'
		  )
		order by started_at, id
		limit 1`, string(app.DeploymentStatusPending))
	deployment, err = scanDeployment(row)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return app.Deployment{}, false, fmt.Errorf("commit empty deployment claim transaction: %w", err)
		}
		return app.Deployment{}, false, nil
	}
	if err != nil {
		return app.Deployment{}, false, fmt.Errorf("read pending deployment: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		update deployments
		set status = ?, finished_at = ?, error_message = ''
		where id = ? and status = ?`,
		string(app.DeploymentStatusDeploying),
		formatTime(time.Time{}),
		deployment.ID,
		string(app.DeploymentStatusPending),
	)
	if err != nil {
		return app.Deployment{}, false, fmt.Errorf("claim pending deployment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return app.Deployment{}, false, fmt.Errorf("count claimed deployment: %w", err)
	}
	if affected == 0 {
		if err := tx.Commit(); err != nil {
			return app.Deployment{}, false, fmt.Errorf("commit contested deployment claim transaction: %w", err)
		}
		return app.Deployment{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return app.Deployment{}, false, fmt.Errorf("commit deployment claim transaction: %w", err)
	}
	deployment.Status = app.DeploymentStatusDeploying
	deployment.FinishedAt = time.Time{}
	deployment.ErrorMessage = ""
	return deployment, true, nil
}

// RecordDeploymentQueued makes the accepted-ref and queued lifecycle records
// visible together, before a worker can claim the pending deployment.
func (s *SQLiteStore) RecordDeploymentQueued(ctx context.Context, accepted app.Event, queued app.Event) (returnErr error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin queued deployment event transaction: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = tx.Rollback()
		}
	}()

	for _, event := range []app.Event{accepted, queued} {
		if _, err := tx.ExecContext(ctx, `
			insert into events (id, app_id, type, message, created_at)
			values (?, ?, ?, ?, ?)`,
			event.ID,
			event.AppID,
			event.Type,
			event.Message,
			formatTime(event.CreatedAt),
		); err != nil {
			return fmt.Errorf("record %s event: %w", event.Type, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit queued deployment events: %w", err)
	}
	return nil
}
