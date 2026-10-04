package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Каждая новая матрица получает пустую мигрированную БД. Fixture не изменяет
// общий installation singleton и не оставляет историю для TestBootstrapComponent.
func isolatedAssistantComponentDSN(t *testing.T) string {
	t.Helper()
	runtimeDSN := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if runtimeDSN == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	adminDSN := os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN")
	if adminDSN == "" {
		t.Fatal("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN is required for an isolated assistant fixture")
	}
	const templateDatabase = "control_plane_component_template"
	if os.Getenv("KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE") != templateDatabase {
		t.Fatal("KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE must identify the empty disposable template")
	}
	admin, err := pgx.ParseConfig(adminDSN)
	if err != nil || admin.Database != "postgres" || admin.User != "postgres" || admin.Port < 1024 ||
		(admin.Host != "localhost" && admin.Host != "127.0.0.1") {
		t.Fatal("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN must identify a loopback disposable administrator")
	}
	runtimeURL, err := url.Parse(runtimeDSN)
	if err != nil || (runtimeURL.Scheme != "postgresql" && runtimeURL.Scheme != "postgres") ||
		runtimeURL.Hostname() != admin.Host || runtimeURL.Port() == "" ||
		strings.TrimPrefix(runtimeURL.Path, "/") != "control_plane" {
		t.Fatal("KODEX_CONTROL_PLANE_TEST_DSN must identify the same loopback fixture")
	}
	runtime, err := pgx.ParseConfig(runtimeDSN)
	if err != nil || runtime.Port != admin.Port || runtime.Host != admin.Host || runtime.User == "postgres" {
		t.Fatal("KODEX_CONTROL_PLANE_TEST_DSN must retain the fixture application identity")
	}
	var randomID [8]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		t.Fatal("allocate isolated assistant database identity")
	}
	database := "kodex_assistant_test_" + hex.EncodeToString(randomID[:])
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	connection, err := pgx.ConnectConfig(ctx, admin)
	if err != nil {
		t.Fatal("connect disposable database administrator")
	}
	defer func() {
		closeContext, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = connection.Close(closeContext)
	}()
	_, err = connection.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()+
		" TEMPLATE "+pgx.Identifier{templateDatabase}.Sanitize()+" OWNER control_plane_owner")
	if err != nil {
		t.Fatal("clone empty migrated assistant database")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
		defer stop()
		connection, err := pgx.ConnectConfig(cleanup, admin)
		if err != nil {
			t.Error("connect disposable administrator for assistant fixture cleanup")
			return
		}
		defer func() { _ = connection.Close(cleanup) }()
		if _, err := connection.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{database}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("remove exact isolated assistant fixture database")
		}
	})
	runtimeURL.Path = "/" + database
	return runtimeURL.String()
}
