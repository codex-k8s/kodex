package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestTelegramNotificationsCatalogCapture exports only schema fingerprints from disposable normal/backup fixtures when an explicit private capture directory is supplied.
// TestTelegramNotificationsCatalogCapture сохраняет только schema fingerprints отдельных normal/backup fixtures при явном приватном каталоге capture.
func TestTelegramNotificationsCatalogCapture(t *testing.T) {
	directory := os.Getenv("KODEX_TELEGRAM_NOTIFICATIONS_SCHEMA_CAPTURE_DIRECTORY")
	if directory == "" {
		t.Skip("schema capture directory is not requested")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("schema capture directory must be a private regular directory")
	}
	query, err := os.ReadFile("../../../../../tools/release/telegram-notifications-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	restoreQuery, err := os.ReadFile("../../../../../tools/release/telegram-notifications-restore-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, backup := range []bool{os.Getenv("KODEX_TELEGRAM_NOTIFICATIONS_SCHEMA_CAPTURE_MODE") == "backup"} {
		name := "normal"
		if backup {
			name = "backup"
		}
		t.Run(name, func(t *testing.T) {
			ctx, database, observer := newWorkflowUpgradeDatabase(t)
			if err := goose.UpToContext(ctx, database, "migrations", 20261114130000); err != nil {
				t.Fatal(err)
			}
			if backup {
				if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader; RESET ROLE"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := observer.Exec(ctx, "SET search_path=pg_catalog"); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"before", "after"} {
				if phase == "after" {
					if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err != nil {
						t.Fatal(err)
					}
				}
				var raw []byte
				if err := observer.QueryRow(ctx, string(query)).Scan(&raw); err != nil {
					t.Fatal("read full schema catalog", err)
				}
				// Private full metadata supports field-level drift diagnosis; business rows and secret values are excluded by the canonical catalog query.
				// Private полные metadata поддерживают диагностику дрейфа по полям; business rows и secret values исключены canonical catalog query.
				selector := strings.LastIndex(string(query), "SELECT jsonb_build_object(\n")
				if selector < 0 {
					t.Fatal("full catalog selector is not canonical")
				}
				var full []byte
				if err := observer.QueryRow(ctx, string(query[:selector])+"SELECT content FROM complete_catalog;").Scan(&full); err != nil {
					t.Fatal("read full schema metadata", err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+"-"+phase+"-full-schema.json"), full, 0600); err != nil {
					t.Fatal(err)
				}
				var normalized []byte
				if err := observer.QueryRow(ctx, string(restoreQuery)).Scan(&normalized); err != nil {
					t.Fatal("read normalized restore catalog", err)
				}
				var value, mapNormalized map[string]any
				if json.Unmarshal(raw, &value) != nil || json.Unmarshal(normalized, &mapNormalized) != nil {
					t.Fatal("schema capture is invalid")
				}
				value["restoreCatalogDigest"] = mapNormalized["catalogDigest"]
				raw, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+"-"+phase+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
