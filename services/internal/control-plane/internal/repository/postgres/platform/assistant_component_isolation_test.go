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
	"github.com/jackc/pgx/v5/pgxpool"
)

// isolatedAssistantComponentDSN requires matching loopback admin/application fixture DSNs and the
// fixed empty migrated template, creates one random isolated database and registers bounded exact
// cleanup; missing runtime opt-in skips the caller test.
// isolatedAssistantComponentDSN требует совпадающие loopback DSN администратора и приложения и
// фиксированный пустой мигрированный шаблон, создаёт одну случайную изолированную БД и
// регистрирует ограниченную точную очистку; отсутствие runtime opt-in пропускает вызывающий тест.
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

// newIsolatedAssistantComponentPool opens a pool using the existing isolated-database allocator
// and the caller's context, terminating the test on a pool configuration error. The allocator's
// validation, skip and exact database cleanup are unchanged. The caller retains its deadline,
// cancellation and deferred pool close, which runs before registered database cleanup.
// newIsolatedAssistantComponentPool открывает пул через существующее выделение изолированной БД
// с контекстом вызывающего и завершает тест при ошибке настройки пула. Проверки, пропуск и точная
// очистка БД в allocator сохраняются. Срок, отмена и отложенное закрытие пула принадлежат тесту;
// закрытие выполняется до зарегистрированной очистки БД.
func newIsolatedAssistantComponentPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
