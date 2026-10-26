package platform

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// queryProjectPurgeAuthorityACL inspects effective LOGIN privileges, including inherited grants,
	// without assuming that revoking PUBLIC also revoked the runtime role's default table grants.
	// queryProjectPurgeAuthorityACL проверяет effective LOGIN privileges, включая наследование;
	// отзыв PUBLIC не считается отзывом default table grants роли runtime.
	//go:embed testdata/sql/project_purge_authority_acl.sql
	queryProjectPurgeAuthorityACL string
	// queryProjectPurgeAuthoritySpoof executes closed mutation probes; every admitted write aborts
	// the probe transaction and cannot leave fabricated authority behind.
	// queryProjectPurgeAuthoritySpoof исполняет закрытый набор mutation probes; любая допущенная
	// запись откатывает транзакцию проверки и не оставляет поддельного разрешения.
	//go:embed testdata/sql/project_purge_authority_spoof.sql
	queryProjectPurgeAuthoritySpoof string
)

// TestProjectPurgeAuthorityComponent proves that the actual runtime LOGIN retains read access
// needed by invoker guards but cannot forge or alter either private purge authority table.
// TestProjectPurgeAuthorityComponent проверяет, что настоящий runtime LOGIN сохраняет чтение,
// необходимое invoker guards, но не может подделать или изменить таблицы purge authority.
func TestProjectPurgeAuthorityComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated purge authority database")
	}
	defer pool.Close()
	rows, err := pool.Query(ctx, queryProjectPurgeAuthorityACL)
	if err != nil {
		t.Fatal("read effective purge authority privileges")
	}
	count := 0
	for rows.Next() {
		var table string
		var canRead, runtimeLogin, authorized bool
		var mutations []string
		if err := rows.Scan(&table, &canRead, &mutations, &runtimeLogin, &authorized); err != nil {
			rows.Close()
			t.Fatal("decode effective purge authority privileges")
		}
		if !canRead || len(mutations) != 0 || !runtimeLogin || authorized {
			rows.Close()
			t.Fatalf("unexpected effective purge authority privileges: table=%s readable=%v writeCount=%d runtimeLogin=%v authorized=%v", table, canRead, len(mutations), runtimeLogin, authorized)
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count != 2 {
		t.Fatal("incomplete purge authority privilege proof")
	}
	if _, err := pool.Exec(ctx, queryProjectPurgeAuthoritySpoof); err != nil {
		t.Fatal("runtime purge authority spoof was not rejected", err)
	}
}
