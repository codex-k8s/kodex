package platform

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newOptInSecurityComponentPool preserves the explicit test DSN opt-in and 30-second deadline.
// The caller checks the raw open error, cancels the context and closes the pool in its original
// order; this helper does not print the DSN, initialize schemas or contact other services.
// newOptInSecurityComponentPool сохраняет явное включение через тестовый DSN и срок 30 секунд.
// Caller проверяет исходную ошибку открытия, отменяет контекст и закрывает pool в прежнем
// порядке; helper не выводит DSN, не создаёт схемы и не обращается к другим сервисам.
func newOptInSecurityComponentPool(t *testing.T) (context.Context, context.CancelFunc, *pgxpool.Pool, error) {
	t.Helper()
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	pool, err := pgxpool.New(ctx, dsn)
	return ctx, cancel, pool, err
}
