package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// OpenSQLiteReadOnly reads an existing database without creating or migrating it.
func OpenSQLiteReadOnly(ctx context.Context, path string) (*SQLiteStore, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect existing SQLite database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SQLite database %q must be a regular file", path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite database path: %w", err)
	}
	dsn := url.URL{Scheme: "file", Path: absolute}
	query := url.Values{"mode": {"ro"}, "_pragma": {fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeoutMilliseconds)}}
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}
