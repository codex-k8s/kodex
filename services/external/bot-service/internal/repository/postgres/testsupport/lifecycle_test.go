package testsupport

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVectorExtensionLifecycleSerializesCleanDatabase(t *testing.T) {
	dsn := FreshDatabaseDSN(t, "vector_race")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к чистой database: %v", err)
	}
	var extensionExists bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from pg_extension where extname = 'vector')`).Scan(&extensionExists); err != nil {
		pool.Close()
		t.Fatalf("исходная проверка extension: %v", err)
	}
	pool.Close()
	if extensionExists {
		t.Fatal("новая database неожиданно содержит extension vector")
	}

	for iteration := 0; iteration < 3; iteration++ {
		start := make(chan struct{})
		errors := make(chan error, 12)
		var wait sync.WaitGroup
		for range 12 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				errors <- EnsureVectorExtension(ctx, dsn)
			}()
		}
		close(start)
		wait.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatalf("iteration %d concurrent extension setup: %v", iteration+1, err)
			}
		}
	}

	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("повторное подключение: %v", err)
	}
	defer pool.Close()
	var extensionCount int
	var typeAvailable bool
	if err := pool.QueryRow(ctx, `select count(*), to_regtype('public.vector') is not null from pg_extension where extname = 'vector'`).Scan(&extensionCount, &typeAvailable); err != nil {
		t.Fatalf("итоговая проверка extension: %v", err)
	}
	if extensionCount != 1 || !typeAvailable {
		t.Fatalf("extension count=%d type_available=%t", extensionCount, typeAvailable)
	}
}
