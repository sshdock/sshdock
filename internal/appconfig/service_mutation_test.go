package appconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestServiceMutationsOwnAuditAndPreserveMetadata(t *testing.T) {
	ctx := context.Background()
	db := newConfigTestStore(t, ctx)
	now := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
	created := now
	createConfigTestApp(t, ctx, db, "my-app", now)
	service := NewService(db, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))
	for _, request := range []SetRequest{
		{AppID: "my-app", Name: "TOKEN", Value: []byte("first-secret")},
		{AppID: "my-app", Name: "TOKEN", Value: []byte("second-secret"), MutatedBy: " sshdock "},
	} {
		if err := service.Set(ctx, request); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
	}
	value, err := db.GetAppConfigValue(ctx, store.AppConfigRef{AppID: "my-app", Name: "TOKEN"})
	if err != nil || !value.CreatedAt.Equal(created) || !value.UpdatedAt.Equal(created.Add(time.Minute)) || value.MutatedBy != "sshdock" {
		t.Fatalf("stored metadata = %#v, %v", value, err)
	}
	if err := service.Unset(ctx, ConfigRef{AppID: "my-app", Name: "TOKEN"}); err != nil {
		t.Fatal(err)
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil || len(events) != 3 {
		t.Fatalf("events = %#v, %v", events, err)
	}
	ids := map[string]bool{}
	for index, event := range events {
		operation := "set"
		if index == 2 {
			operation = "unset"
		}
		if event.Type != "config."+operation || event.Message != "Config "+operation+" for TOKEN" || event.CreatedAt != created.Add(time.Duration(index)*time.Minute) || ids[event.ID] {
			t.Fatalf("event = %#v", event)
		}
		ids[event.ID] = true
		if strings.Contains(event.Message, "secret") {
			t.Fatal("audit exposed a value")
		}
	}
	if err := service.Unset(ctx, ConfigRef{AppID: "my-app", Name: "TOKEN"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Unset absent value = %v", err)
	}
	after, _ := db.ListEventsByApp(ctx, "my-app")
	if len(after) != len(events) {
		t.Fatalf("failed unset added audit: %#v", after)
	}
}

func TestServiceImportCountsCommittedPrefix(t *testing.T) {
	ctx := context.Background()
	db := newConfigTestStore(t, ctx)
	createConfigTestApp(t, ctx, db, "my-app", time.Now().UTC())
	service := NewService(db, filepath.Join(t.TempDir(), "config.key"))
	count, err := service.Import(ctx, ImportRequest{AppID: "my-app", Input: strings.NewReader("# comment\n FIRST =one=two \nFIRST=second\ninvalid\nLATER=unused\n"), MutatedBy: "sshdock"})
	if count != 2 || err == nil || !strings.Contains(err.Error(), "line 4 must be KEY=VALUE") {
		t.Fatalf("Import = %d, %v", count, err)
	}
	value, err := service.Reveal(ctx, ConfigRef{AppID: "my-app", Name: "FIRST"})
	if err != nil || value != "second" {
		t.Fatalf("committed prefix = %q, %v", value, err)
	}
	if _, err := service.Reveal(ctx, ConfigRef{AppID: "my-app", Name: "LATER"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("later line = %v", err)
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil || len(events) != count {
		t.Fatalf("events = %#v, %v", events, err)
	}
	count, err = service.Import(ctx, ImportRequest{AppID: "my-app", Input: strings.NewReader("SAFE=value\nPATH=blocked\n")})
	if count != 1 || err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved import = %d, %v", count, err)
	}
}

func TestServiceEmptyImportRequiresAppWithoutCreatingKey(t *testing.T) {
	ctx := context.Background()
	db := newConfigTestStore(t, ctx)
	keyPath := filepath.Join(t.TempDir(), "config.key")
	service := NewService(db, keyPath)
	for _, appID := range []string{"missing", " "} {
		if count, err := service.Import(ctx, ImportRequest{AppID: appID}); count != 0 || err == nil {
			t.Fatalf("Import(%q) = %d, %v", appID, count, err)
		}
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-app import created key: %v", err)
	}
	createConfigTestApp(t, ctx, db, "my-app", time.Now().UTC())
	if count, err := service.Import(ctx, ImportRequest{AppID: "my-app"}); count != 0 || err != nil {
		t.Fatalf("empty existing-app import = %d, %v", count, err)
	}
}

func TestServiceCanRedactAndUnsetStoredReservedValue(t *testing.T) {
	ctx := context.Background()
	db := newConfigTestStore(t, ctx)
	now := time.Now().UTC()
	createConfigTestApp(t, ctx, db, "my-app", now)
	keyPath := filepath.Join(t.TempDir(), "config.key")
	key, err := LoadOrCreateHostKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ref := ConfigRef{AppID: "my-app", Name: "PATH"}
	box, err := Encrypt(ref, key, []byte("unsafe-stored-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAppConfigValue(ctx, store.AppConfigValue{AppID: ref.AppID, Name: ref.Name, Ciphertext: box.Ciphertext, Nonce: box.Nonce, KeyVersion: box.KeyVersion, CreatedAt: now, UpdatedAt: now}, appmodel.Event{ID: "fixture", AppID: ref.AppID, Type: "config.set", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, keyPath)
	if _, err := service.ResolveEnv(ctx, "my-app"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("ResolveEnv unsafe key = %v", err)
	}
	values, err := service.RedactionValues(ctx, "my-app")
	if err != nil || values["my-app/PATH"] != "unsafe-stored-secret" {
		t.Fatalf("redactions = %#v, %v", values, err)
	}
	if err := service.Unset(ctx, ref); err != nil {
		t.Fatalf("Unset unsafe stored key = %v", err)
	}
}

func TestImportLinesPreservesGrammarAndReadErrors(t *testing.T) {
	var got []string
	count, err := ImportLines(strings.NewReader(" # ignored\n A =one=two \nEMPTY=\nQUOTED=\"literal\"\n"), func(name string, value []byte) error {
		got = append(got, name+"="+string(value))
		return nil
	})
	if err != nil || count != 3 || strings.Join(got, "|") != "A=one=two|EMPTY=|QUOTED=\"literal\"" {
		t.Fatalf("ImportLines = %d, %v, %#v", count, err, got)
	}
	count, err = ImportLines(strings.NewReader("OK=value\n"+strings.Repeat("x", 70*1024)), func(string, []byte) error { return nil })
	if count != 1 || err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("long import = %d, %v", count, err)
	}
}
