//go:build postgres

package testsupport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresMandatoryExecutionSentinel(t *testing.T) {
	dsn := RequiredDSN(t)
	major := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_MAJOR"))
	proofPath := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_SENTINEL_PATH"))
	proofValue := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_SENTINEL_VALUE"))
	if (major != "15" && major != "16") || proofPath == "" || proofValue == "" || !filepath.IsAbs(proofPath) {
		t.Fatal("обязательный PostgreSQL sentinel не получил server-owned identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ValidatePostgresMajor(ctx, dsn, major); err != nil {
		t.Fatal("обязательный PostgreSQL sentinel получил несовпадающую major-версию")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("обязательный PostgreSQL sentinel не подключился к disposable target")
	}
	defer pool.Close()
	var extensionCount int
	if err := pool.QueryRow(ctx, `select count(*) from pg_available_extensions where name in ('vector', 'amcheck')`).Scan(&extensionCount); err != nil || extensionCount != 2 {
		t.Fatal("обязательный PostgreSQL sentinel не подтвердил extensions vector и amcheck")
	}
	file, err := os.OpenFile(proofPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("обязательный PostgreSQL sentinel не создал exact proof")
	}
	if _, err := file.WriteString(major + "\n" + proofValue + "\n"); err != nil {
		_ = file.Close()
		t.Fatal("обязательный PostgreSQL sentinel не записал exact proof")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal("обязательный PostgreSQL sentinel не синхронизировал exact proof")
	}
	if err := file.Close(); err != nil {
		t.Fatal("обязательный PostgreSQL sentinel не закрыл exact proof")
	}
}
