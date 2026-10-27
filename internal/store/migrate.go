package store

import (
	"context"
	"database/sql"
	"fmt"
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
			commit_sha text not null,
			trigger text not null,
			status text not null,
			started_at text not null,
			finished_at text not null,
			failure_stage text not null,
			failure_detail text not null,
			retry_guidance text not null,
			error_message text not null
		)`,
		`create table if not exists deployment_logs (
			deployment_id text primary key,
			app_id text not null,
			content text not null,
			truncated integer not null,
			created_at text not null,
			updated_at text not null
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
		`create table if not exists route_apply_failures (
			app_id text not null,
			service_name text not null,
			domain_name text not null,
			port integer not null,
			https integer not null,
			operation text not null,
			detail text not null,
			updated_at text not null,
			primary key (app_id, domain_name)
		)`,
		`create table if not exists server_config (
			key text primary key,
			value text not null,
			updated_at text not null
		)`,
		`create table if not exists ssh_keys (
			name text primary key,
			public_key text not null,
			created_at text not null
		)`,
		`create table if not exists app_config_values (
			app_id text not null,
			name text not null,
			ciphertext blob not null,
			nonce blob not null,
			key_version integer not null,
			created_at text not null,
			updated_at text not null,
			mutated_by text not null,
			primary key (app_id, name)
		)`,
		`create index if not exists idx_releases_app_id on releases(app_id)`,
		`create unique index if not exists idx_releases_app_commit on releases(app_id, commit_sha)`,
		`create index if not exists idx_deployments_app_started on deployments(app_id, started_at, id)`,
		`create index if not exists idx_deployment_logs_app_created on deployment_logs(app_id, created_at, deployment_id)`,
		`create index if not exists idx_domains_app_id on domains(app_id)`,
		`create index if not exists idx_events_app_id on events(app_id)`,
		`create index if not exists idx_route_apply_failures_app_id on route_apply_failures(app_id)`,
		`create trigger if not exists delete_route_apply_failures_after_app_delete
		after delete on apps
		begin
			delete from route_apply_failures where app_id = old.id;
		end`,
		`create index if not exists idx_app_config_values_app_id on app_config_values(app_id)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}

	return nil
}
