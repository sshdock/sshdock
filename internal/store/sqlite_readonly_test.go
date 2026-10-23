package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenSQLiteReadOnlyDoesNotCreateMissingDatabase(t *testing.T) {
	for _, relative := range []string{"missing.db", "missing-parent/missing.db"} {
		t.Run(relative, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, relative)
			if db, err := OpenSQLiteReadOnly(context.Background(), path); !errors.Is(err, os.ErrNotExist) || db != nil {
				t.Fatalf("OpenSQLiteReadOnly = %v, %v", db, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("missing database check changed directory: %v, %v", entries, err)
			}
		})
	}
}

func TestOpenSQLiteReadOnlyPreservesLegacySchemaAndRejectsWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy ?readonly.db")
	dsn := url.URL{Scheme: "file", Path: path}
	seed, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"create table proof (value text not null)",
		"insert into proof values ('retained')",
	} {
		if _, err := seed.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenSQLiteReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.db.QueryRowContext(ctx, "select value from proof").Scan(&value); err != nil || value != "retained" {
		t.Fatalf("read legacy database: value=%q err=%v", value, err)
	}
	var names []string
	rows, err := db.db.QueryContext(ctx, "select name from sqlite_schema order by name")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"proof"}) {
		t.Fatalf("read-only open migrated schema: %v", names)
	}
	for _, statement := range []string{"insert into proof values ('must fail')", "create table forbidden (value text)"} {
		if _, err := db.db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("read-only database accepted %q", statement)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() || beforeInfo.ModTime() != afterInfo.ModTime() {
		t.Fatal("read-only open changed database contents or metadata")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("read-only open created database side files: %v, %v", entries, err)
	}
}
