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
		`create index if not exists idx_domains_app_id on domains(app_id)`,
		`create index if not exists idx_events_app_id on events(app_id)`,
		`create index if not exists idx_app_config_values_app_id on app_config_values(app_id)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}

	if err := migrateLegacyDeployments(ctx, db); err != nil {
		return err
	}
	return migrateLegacyAppConfig(ctx, db)
}

func migrateLegacyDeployments(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "commit_sha", definition: "text not null default ''"},
		{name: "trigger", definition: "text not null default 'legacy'"},
		{name: "failure_stage", definition: "text not null default ''"},
		{name: "failure_detail", definition: "text not null default ''"},
		{name: "retry_guidance", definition: "text not null default ''"},
	}
	for _, column := range columns {
		exists, err := tableColumnExists(ctx, db, "deployments", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		statement := "alter table deployments add column " + column.name + " " + column.definition
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add deployments.%s: %w", column.name, err)
		}
	}

	statements := []string{
		`update deployments set trigger = 'legacy' where trigger = ''`,
		`update deployments
		 set commit_sha = coalesce((select releases.commit_sha from releases where releases.id = deployments.release_id), '')
		 where commit_sha = ''`,
		`update deployments set failure_stage = 'legacy' where status = 'failed' and failure_stage = ''`,
		`update deployments set failure_detail = error_message where failure_detail = '' and error_message != ''`,
		`update deployments
		 set retry_guidance = 'inspect failure detail and retry the original deployment operation'
		 where status = 'failed' and retry_guidance = ''`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("backfill deployment attempts: %w", err)
		}
	}
	return nil
}

func tableColumnExists(ctx context.Context, db *sql.DB, table string, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "pragma table_info("+table+")")
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var sequence int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&sequence, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	return false, nil
}
