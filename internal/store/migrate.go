package store

import (
	"context"
	"database/sql"
)

func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`create table if not exists apps (
			id text primary key,
			name text not null,
			node_id text not null,
			repo_path text not null,
			worktree_path text not null,
			compose_path text not null,
			status text not null,
			created_at text not null,
			updated_at text not null
		)`,
		`create table if not exists releases (
			id text primary key,
			app_id text not null,
			commit_sha text not null,
			compose_path text not null,
			status text not null,
			created_at text not null,
			updated_at text not null
		)`,
		`create table if not exists deployments (
			id text primary key,
			app_id text not null,
			release_id text not null,
			status text not null,
			started_at text not null,
			finished_at text not null,
			error_message text not null
		)`,
		`create table if not exists domains (
			id text primary key,
			app_id text not null,
			service_name text not null,
			domain_name text not null,
			port integer not null,
			https integer not null,
			created_at text not null,
			updated_at text not null
		)`,
		`create table if not exists events (
			id text primary key,
			app_id text not null,
			type text not null,
			message text not null,
			created_at text not null
		)`,
		`create index if not exists idx_releases_app_id on releases(app_id)`,
		`create index if not exists idx_domains_app_id on domains(app_id)`,
		`create index if not exists idx_events_app_id on events(app_id)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}
