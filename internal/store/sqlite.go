package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
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

func (s *SQLiteStore) CreateDeployment(ctx context.Context, model app.Deployment) error {
	_, err := s.db.ExecContext(ctx, `
		insert into deployments (id, app_id, release_id, status, started_at, finished_at, error_message)
		values (?, ?, ?, ?, ?, ?, ?)`,
		model.ID,
		model.AppID,
		model.ReleaseID,
		string(model.Status),
		formatTime(model.StartedAt),
		formatTime(model.FinishedAt),
		model.ErrorMessage,
	)
	return err
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

func (s *SQLiteStore) AttachDomain(ctx context.Context, model app.Domain) error {
	_, err := s.db.ExecContext(ctx, `
		insert into domains (id, app_id, service_name, domain_name, port, https, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)`,
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
	row := s.db.QueryRowContext(ctx, `
		select value, updated_at
		from server_config
		where key = 'git_host'`)

	var config ServerConfig
	var updatedAt string
	if err := row.Scan(&config.GitHost, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ServerConfig{}, notFound("server config", "git_host")
		}
		return ServerConfig{}, err
	}

	parsed, err := parseTime(updatedAt)
	if err != nil {
		return ServerConfig{}, err
	}
	config.UpdatedAt = parsed
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

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
