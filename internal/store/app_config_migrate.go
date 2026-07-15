package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sshdock/sshdock/internal/appconfig/legacycipher"
)

type legacyAppConfigValue struct {
	appID      string
	name       string
	scope      string
	ciphertext []byte
	nonce      []byte
	keyVersion int
	createdAt  string
	updatedAt  string
	mutatedBy  string
}

func migrateLegacyAppConfig(ctx context.Context, db *sql.DB) error {
	hasScope, err := tableColumnExists(ctx, db, "app_config_values", "scope")
	if err != nil {
		return err
	}
	if !hasScope {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin app config migration: %w", err)
	}
	defer tx.Rollback()

	if err := rejectConflictingLegacyAppConfig(ctx, tx); err != nil {
		return err
	}
	values, err := loadLegacyAppConfig(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		create table app_config_values_flat_migration (
			app_id text not null,
			name text not null,
			ciphertext blob not null,
			nonce blob not null,
			key_version integer not null,
			created_at text not null,
			updated_at text not null,
			mutated_by text not null,
			primary key (app_id, name)
		)`); err != nil {
		return fmt.Errorf("create flat app config table: %w", err)
	}
	for _, value := range values {
		ciphertext := value.ciphertext
		if value.scope != "" {
			ciphertext, err = legacycipher.Wrap(value.scope, value.ciphertext)
			if err != nil {
				return fmt.Errorf("preserve legacy encryption context for app %q key %q: %w", value.appID, value.name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			insert into app_config_values_flat_migration
			(app_id, name, ciphertext, nonce, key_version, created_at, updated_at, mutated_by)
			values (?, ?, ?, ?, ?, ?, ?, ?)`,
			value.appID, value.name, ciphertext, value.nonce, value.keyVersion,
			value.createdAt, value.updatedAt, value.mutatedBy,
		); err != nil {
			return fmt.Errorf("migrate config for app %q key %q: %w", value.appID, value.name, err)
		}
	}
	for _, statement := range []string{
		`drop table app_config_values`,
		`alter table app_config_values_flat_migration rename to app_config_values`,
		`create index idx_app_config_values_app_id on app_config_values(app_id)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("finalize flat app config migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit app config migration: %w", err)
	}
	return nil
}

func rejectConflictingLegacyAppConfig(ctx context.Context, tx *sql.Tx) error {
	var appID string
	var name string
	err := tx.QueryRowContext(ctx, `
		select app_id, name
		from app_config_values
		group by app_id, name
		having count(*) > 1
		order by app_id, name
		limit 1`).Scan(&appID, &name)
	if err == nil {
		scopes, scopesErr := legacyConfigScopes(ctx, tx, appID, name)
		if scopesErr != nil {
			return scopesErr
		}
		return fmt.Errorf(
			"app config migration blocked for app %q key %q; scopes: %s; remove all but one value with the previous SSHDock version, then retry",
			appID,
			name,
			strings.Join(scopes, ", "),
		)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("inspect legacy app config conflicts: %w", err)
	}
	return nil
}

func legacyConfigScopes(ctx context.Context, tx *sql.Tx, appID string, name string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `select scope from app_config_values where app_id = ? and name = ? order by scope`, appID, name)
	if err != nil {
		return nil, fmt.Errorf("list conflicting app config scopes: %w", err)
	}
	defer rows.Close()

	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("scan conflicting app config scope: %w", err)
		}
		if scope == "" {
			scope = "<flat>"
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list conflicting app config scopes: %w", err)
	}
	return scopes, nil
}

func loadLegacyAppConfig(ctx context.Context, tx *sql.Tx) ([]legacyAppConfigValue, error) {
	rows, err := tx.QueryContext(ctx, `
		select app_id, name, scope, ciphertext, nonce, key_version, created_at, updated_at, mutated_by
		from app_config_values
		order by app_id, name`)
	if err != nil {
		return nil, fmt.Errorf("list legacy app config: %w", err)
	}
	defer rows.Close()

	var values []legacyAppConfigValue
	for rows.Next() {
		var value legacyAppConfigValue
		if err := rows.Scan(
			&value.appID,
			&value.name,
			&value.scope,
			&value.ciphertext,
			&value.nonce,
			&value.keyVersion,
			&value.createdAt,
			&value.updatedAt,
			&value.mutatedBy,
		); err != nil {
			return nil, fmt.Errorf("scan legacy app config: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list legacy app config: %w", err)
	}
	return values, nil
}
