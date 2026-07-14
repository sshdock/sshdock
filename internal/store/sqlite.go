package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) CreateApp(ctx context.Context, model app.App) error {
	_, err := s.db.ExecContext(ctx, `
		insert into apps (id, name, node_id, repo_path, worktree_path, compose_path, status, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		model.ID,
		model.Name,
		model.NodeID,
		model.RepoPath,
		model.WorktreePath,
		model.ComposePath,
		string(model.Status),
		formatTime(model.CreatedAt),
		formatTime(model.UpdatedAt),
	)
	return err
}

func (s *SQLiteStore) GetApp(ctx context.Context, id string) (app.App, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, name, node_id, repo_path, worktree_path, compose_path, status, created_at, updated_at
		from apps
		where id = ?`, id)

	model, err := scanApp(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.App{}, notFound("app", id)
	}

	return model, err
}

func (s *SQLiteStore) ListApps(ctx context.Context) ([]app.App, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, name, node_id, repo_path, worktree_path, compose_path, status, created_at, updated_at
		from apps
		order by created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []app.App
	for rows.Next() {
		model, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, rows.Err()
}

func (s *SQLiteStore) UpdateAppStatus(ctx context.Context, id string, status app.AppStatus, updatedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		update apps
		set status = ?, updated_at = ?
		where id = ?`,
		string(status),
		formatTime(updatedAt),
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound("app", id)
	}

	return nil
}

func (s *SQLiteStore) CreateRelease(ctx context.Context, model app.Release) error {
	_, err := s.db.ExecContext(ctx, `
		insert into releases (id, app_id, commit_sha, compose_path, status, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?)`,
		model.ID,
		model.AppID,
		model.CommitSHA,
		model.ComposePath,
		string(model.Status),
		formatTime(model.CreatedAt),
		formatTime(model.UpdatedAt),
	)
	return err
}

func (s *SQLiteStore) GetRelease(ctx context.Context, id string) (app.Release, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, app_id, commit_sha, compose_path, status, created_at, updated_at
		from releases
		where id = ?`, id)

	model, err := scanRelease(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Release{}, notFound("release", id)
	}

	return model, err
}

func (s *SQLiteStore) GetReleaseByAppCommit(ctx context.Context, appID string, commitSHA string) (app.Release, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, app_id, commit_sha, compose_path, status, created_at, updated_at
		from releases
		where app_id = ? and commit_sha = ?`, appID, commitSHA)

	model, err := scanRelease(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Release{}, notFound("release for app commit", appID+"/"+commitSHA)
	}
	return model, err
}

func (s *SQLiteStore) ListReleasesByApp(ctx context.Context, appID string) ([]app.Release, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, app_id, commit_sha, compose_path, status, created_at, updated_at
		from releases
		where app_id = ?
		order by created_at, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []app.Release
	for rows.Next() {
		model, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, rows.Err()
}

func (s *SQLiteStore) UpdateReleaseStatus(ctx context.Context, id string, status app.ReleaseStatus, updatedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		update releases
		set status = ?, updated_at = ?
		where id = ?`,
		string(status),
		formatTime(updatedAt),
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound("release", id)
	}

	return nil
}

func (s *SQLiteStore) MarkReleaseFailedUnlessGood(ctx context.Context, id string, updatedAt time.Time) error {
	return s.markReleaseStatusUnlessGood(ctx, id, app.ReleaseStatusFailed, updatedAt)
}

func (s *SQLiteStore) MarkReleaseDeployingUnlessGood(ctx context.Context, id string, updatedAt time.Time) error {
	return s.markReleaseStatusUnlessGood(ctx, id, app.ReleaseStatusDeploying, updatedAt)
}

func (s *SQLiteStore) markReleaseStatusUnlessGood(ctx context.Context, id string, status app.ReleaseStatus, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		update releases
		set status = ?, updated_at = ?
		where id = ? and status not in (?, ?)`,
		string(status),
		formatTime(updatedAt),
		id,
		string(app.ReleaseStatusSucceeded),
		string(app.ReleaseStatusRolledBack),
	)
	return err
}

func (s *SQLiteStore) CreateDeployment(ctx context.Context, model app.Deployment) error {
	_, err := s.db.ExecContext(ctx, `
		insert into deployments (
			id, app_id, release_id, commit_sha, trigger, status, started_at, finished_at,
			failure_stage, failure_detail, retry_guidance, error_message
		)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		model.ID,
		model.AppID,
		model.ReleaseID,
		model.CommitSHA,
		string(model.Trigger),
		string(model.Status),
		formatTime(model.StartedAt),
		formatTime(model.FinishedAt),
		model.FailureStage,
		model.FailureDetail,
		model.RetryGuidance,
		model.ErrorMessage,
	)
	return err
}

func (s *SQLiteStore) ListDeploymentsByApp(ctx context.Context, appID string) ([]app.Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, app_id, release_id, commit_sha, trigger, status, started_at, finished_at,
		       failure_stage, failure_detail, retry_guidance, error_message
		from deployments
		where app_id = ?
		order by started_at, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []app.Deployment
	for rows.Next() {
		model, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, rows.Err()
}

func (s *SQLiteStore) UpdateDeploymentStatus(ctx context.Context, id string, status app.DeploymentStatus, finishedAt time.Time, errorMessage string) error {
	result, err := s.db.ExecContext(ctx, `
		update deployments
		set status = ?, finished_at = ?, error_message = ?
		where id = ?`,
		string(status),
		formatTime(finishedAt),
		errorMessage,
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound("deployment", id)
	}

	return nil
}

func (s *SQLiteStore) UpdateDeploymentFailure(ctx context.Context, model app.Deployment) error {
	result, err := s.db.ExecContext(ctx, `
		update deployments
		set status = ?, finished_at = ?, failure_stage = ?, failure_detail = ?, retry_guidance = ?, error_message = ?
		where id = ?`,
		string(app.DeploymentStatusFailed),
		formatTime(model.FinishedAt),
		model.FailureStage,
		model.FailureDetail,
		model.RetryGuidance,
		model.FailureDetail,
		model.ID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound("deployment", model.ID)
	}
	return nil
}

func (s *SQLiteStore) AttachDomain(ctx context.Context, model app.Domain) error {
	_, err := s.db.ExecContext(ctx, `
		insert into domains (id, app_id, service_name, domain_name, port, https, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(id) do update set
			app_id = excluded.app_id,
			service_name = excluded.service_name,
			domain_name = excluded.domain_name,
			port = excluded.port,
			https = excluded.https,
			updated_at = excluded.updated_at`,
		model.ID,
		model.AppID,
		model.ServiceName,
		model.DomainName,
		model.Port,
		model.HTTPS,
		formatTime(model.CreatedAt),
		formatTime(model.UpdatedAt),
	)
	return err
}

func (s *SQLiteStore) ListDomains(ctx context.Context) ([]app.Domain, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, app_id, service_name, domain_name, port, https, created_at, updated_at
		from domains
		order by created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []app.Domain
	for rows.Next() {
		model, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, rows.Err()
}

func (s *SQLiteStore) ListDomainsByApp(ctx context.Context, appID string) ([]app.Domain, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, app_id, service_name, domain_name, port, https, created_at, updated_at
		from domains
		where app_id = ?
		order by created_at, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []app.Domain
	for rows.Next() {
		model, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, rows.Err()
}

func (s *SQLiteStore) DeleteDomainByAppAndName(ctx context.Context, appID string, domainName string) (app.Domain, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, app_id, service_name, domain_name, port, https, created_at, updated_at
		from domains
		where app_id = ? and domain_name = ?`, appID, domainName)
	model, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Domain{}, notFound("domain", domainName)
	}
	if err != nil {
		return app.Domain{}, err
	}

	result, err := s.db.ExecContext(ctx, `
		delete from domains
		where app_id = ? and domain_name = ?`, appID, domainName)
	if err != nil {
		return app.Domain{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return app.Domain{}, err
	}
	if affected == 0 {
		return app.Domain{}, notFound("domain", domainName)
	}

	return model, nil
}

func (s *SQLiteStore) CreateEvent(ctx context.Context, model app.Event) error {
	_, err := s.db.ExecContext(ctx, `
		insert into events (id, app_id, type, message, created_at)
		values (?, ?, ?, ?, ?)`,
		model.ID,
		model.AppID,
		model.Type,
		model.Message,
		formatTime(model.CreatedAt),
	)
	return err
}

func (s *SQLiteStore) ListEventsByApp(ctx context.Context, appID string) ([]app.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, app_id, type, message, created_at
		from events
		where app_id = ?
		order by created_at, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []app.Event
	for rows.Next() {
		model, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, rows.Err()
}

func (s *SQLiteStore) SetServerConfig(ctx context.Context, config ServerConfig) error {
	if config.BaseDomain == "" && config.GitHost == "" {
		return fmt.Errorf("server config requires base domain or Git host")
	}
	if config.BaseDomain != "" {
		if _, err := s.db.ExecContext(ctx, `
			insert into server_config (key, value, updated_at)
			values ('base_domain', ?, ?)
			on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`,
			config.BaseDomain,
			formatTime(config.UpdatedAt),
		); err != nil {
			return err
		}
	}
	if config.GitHost == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		insert into server_config (key, value, updated_at)
		values ('git_host', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`,
		config.GitHost,
		formatTime(config.UpdatedAt),
	)
	return err
}

func (s *SQLiteStore) GetServerConfig(ctx context.Context) (ServerConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		select key, value, updated_at
		from server_config
		where key in ('base_domain', 'git_host')`)
	if err != nil {
		return ServerConfig{}, err
	}
	defer rows.Close()

	var config ServerConfig
	var found bool
	for rows.Next() {
		var key string
		var value string
		var updatedAt string
		if err := rows.Scan(&key, &value, &updatedAt); err != nil {
			return ServerConfig{}, err
		}
		parsed, err := parseTime(updatedAt)
		if err != nil {
			return ServerConfig{}, err
		}
		switch key {
		case "base_domain":
			config.BaseDomain = value
		case "git_host":
			config.GitHost = value
		}
		if !found || parsed.After(config.UpdatedAt) {
			config.UpdatedAt = parsed
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return ServerConfig{}, err
	}
	if !found {
		return ServerConfig{}, notFound("server config", "server_config")
	}
	return config, nil
}

func (s *SQLiteStore) UpsertSSHKey(ctx context.Context, key SSHKey) error {
	_, err := s.db.ExecContext(ctx, `
		insert into ssh_keys (name, public_key, created_at)
		values (?, ?, ?)
		on conflict(name) do update set public_key = excluded.public_key, created_at = excluded.created_at`,
		key.Name,
		key.PublicKey,
		formatTime(key.CreatedAt),
	)
	return err
}

func (s *SQLiteStore) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		select name, public_key, created_at
		from ssh_keys
		order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []SSHKey
	for rows.Next() {
		var key SSHKey
		var createdAt string
		if err := rows.Scan(&key.Name, &key.PublicKey, &createdAt); err != nil {
			return nil, err
		}
		parsed, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		key.CreatedAt = parsed
		keys = append(keys, key)
	}

	return keys, rows.Err()
}

func (s *SQLiteStore) DeleteSSHKey(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, `
		delete from ssh_keys
		where name = ?`, name)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound("SSH key", name)
	}
	return nil
}

func (s *SQLiteStore) UpsertAppConfigValue(ctx context.Context, value AppConfigValue) error {
	_, err := s.db.ExecContext(ctx, `
		insert into app_config_values (app_id, name, scope, ciphertext, nonce, key_version, created_at, updated_at, mutated_by)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(app_id, name, scope) do update set
			ciphertext = excluded.ciphertext,
			nonce = excluded.nonce,
			key_version = excluded.key_version,
			updated_at = excluded.updated_at,
			mutated_by = excluded.mutated_by`,
		value.AppID,
		value.Name,
		value.Scope,
		value.Ciphertext,
		value.Nonce,
		value.KeyVersion,
		formatTime(value.CreatedAt),
		formatTime(value.UpdatedAt),
		value.MutatedBy,
	)
	return err
}

func (s *SQLiteStore) GetAppConfigValue(ctx context.Context, ref AppConfigRef) (AppConfigValue, error) {
	row := s.db.QueryRowContext(ctx, `
		select app_id, name, scope, ciphertext, nonce, key_version, created_at, updated_at, mutated_by
		from app_config_values
		where app_id = ? and name = ? and scope = ?`,
		ref.AppID,
		ref.Name,
		ref.Scope,
	)
	value, err := scanAppConfigValue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AppConfigValue{}, notFound("app config value", ref.AppID+"/"+ref.Scope+"/"+ref.Name)
	}
	return value, err
}

func (s *SQLiteStore) ListAppConfigValues(ctx context.Context, appID string) ([]AppConfigValue, error) {
	rows, err := s.db.QueryContext(ctx, `
		select app_id, name, scope, ciphertext, nonce, key_version, created_at, updated_at, mutated_by
		from app_config_values
		where app_id = ?
		order by scope, name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []AppConfigValue
	for rows.Next() {
		value, err := scanAppConfigValue(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *SQLiteStore) DeleteAppConfigValue(ctx context.Context, ref AppConfigRef) error {
	result, err := s.db.ExecContext(ctx, `
		delete from app_config_values
		where app_id = ? and name = ? and scope = ?`,
		ref.AppID,
		ref.Name,
		ref.Scope,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound("app config value", ref.AppID+"/"+ref.Scope+"/"+ref.Name)
	}
	return nil
}

func (s *SQLiteStore) DeleteApp(ctx context.Context, appID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id string
	if err := tx.QueryRowContext(ctx, `select id from apps where id = ?`, appID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("app", appID)
		}
		return err
	}

	for _, statement := range []string{
		`delete from events where app_id = ?`,
		`delete from deployments where app_id = ?`,
		`delete from releases where app_id = ?`,
		`delete from domains where app_id = ?`,
		`delete from app_config_values where app_id = ?`,
		`delete from apps where id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, appID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApp(s scanner) (app.App, error) {
	var model app.App
	var status string
	var createdAt string
	var updatedAt string
	err := s.Scan(
		&model.ID,
		&model.Name,
		&model.NodeID,
		&model.RepoPath,
		&model.WorktreePath,
		&model.ComposePath,
		&status,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return app.App{}, err
	}

	var parseErr error
	model.Status = app.AppStatus(status)
	model.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return app.App{}, parseErr
	}
	model.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return app.App{}, parseErr
	}

	return model, nil
}

func scanRelease(s scanner) (app.Release, error) {
	var model app.Release
	var status string
	var createdAt string
	var updatedAt string
	err := s.Scan(
		&model.ID,
		&model.AppID,
		&model.CommitSHA,
		&model.ComposePath,
		&status,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return app.Release{}, err
	}

	var parseErr error
	model.Status = app.ReleaseStatus(status)
	model.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return app.Release{}, parseErr
	}
	model.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return app.Release{}, parseErr
	}

	return model, nil
}

func scanDeployment(s scanner) (app.Deployment, error) {
	var model app.Deployment
	var trigger string
	var status string
	var startedAt string
	var finishedAt string
	err := s.Scan(
		&model.ID,
		&model.AppID,
		&model.ReleaseID,
		&model.CommitSHA,
		&trigger,
		&status,
		&startedAt,
		&finishedAt,
		&model.FailureStage,
		&model.FailureDetail,
		&model.RetryGuidance,
		&model.ErrorMessage,
	)
	if err != nil {
		return app.Deployment{}, err
	}

	var parseErr error
	model.Trigger = app.DeploymentTrigger(trigger)
	model.Status = app.DeploymentStatus(status)
	model.StartedAt, parseErr = parseTime(startedAt)
	if parseErr != nil {
		return app.Deployment{}, parseErr
	}
	model.FinishedAt, parseErr = parseTime(finishedAt)
	if parseErr != nil {
		return app.Deployment{}, parseErr
	}

	return model, nil
}

func scanDomain(s scanner) (app.Domain, error) {
	var model app.Domain
	var createdAt string
	var updatedAt string
	err := s.Scan(
		&model.ID,
		&model.AppID,
		&model.ServiceName,
		&model.DomainName,
		&model.Port,
		&model.HTTPS,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return app.Domain{}, err
	}

	var parseErr error
	model.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return app.Domain{}, parseErr
	}
	model.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return app.Domain{}, parseErr
	}

	return model, nil
}

func scanEvent(s scanner) (app.Event, error) {
	var model app.Event
	var createdAt string
	err := s.Scan(
		&model.ID,
		&model.AppID,
		&model.Type,
		&model.Message,
		&createdAt,
	)
	if err != nil {
		return app.Event{}, err
	}

	var parseErr error
	model.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return app.Event{}, parseErr
	}

	return model, nil
}

func scanAppConfigValue(s scanner) (AppConfigValue, error) {
	var value AppConfigValue
	var createdAt string
	var updatedAt string
	err := s.Scan(
		&value.AppID,
		&value.Name,
		&value.Scope,
		&value.Ciphertext,
		&value.Nonce,
		&value.KeyVersion,
		&createdAt,
		&updatedAt,
		&value.MutatedBy,
	)
	if err != nil {
		return AppConfigValue{}, err
	}

	var parseErr error
	value.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return AppConfigValue{}, parseErr
	}
	value.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return AppConfigValue{}, parseErr
	}

	return value, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
