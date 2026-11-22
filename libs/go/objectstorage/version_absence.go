package objectstorage

import (
	"context"
	"strings"
)

// VersionAbsenceVerifier attests absence of one owner-pinned version in the adapter's exact target.
// A generic HTTP404, permission failure or unknown endpoint is not such proof. Implementations must
// qualify their target/authentication boundary without widening caller permissions or selecting latest.
// RU: VersionAbsenceVerifier подтверждает отсутствие owner-pinned версии в точном target адаптера.
// Generic HTTP404, отказ доступа и неизвестный endpoint доказательством не являются. Реализация
// квалифицирует target/authentication boundary без расширения полномочий и выбора latest.
type VersionAbsenceVerifier interface {
	// VerifyVersionAbsent returns nil only for qualified absence; uncertainty stays an error.
	// RU: VerifyVersionAbsent возвращает nil лишь при доказанном отсутствии; неопределённость остаётся ошибкой.
	VerifyVersionAbsent(context.Context, string, string) error
}

// VerifyVersionAbsent dispatches only to an explicit qualified verifier, never to generic Head.
// S3 adapters without a qualified target proof therefore remain closed after a lost Delete response.
// RU: VerifyVersionAbsent вызывает только явный qualified verifier, без fallback на generic Head.
// S3 без квалифицированного target proof остаётся закрытым после потерянного ответа Delete.
func VerifyVersionAbsent(ctx context.Context, store any, key, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ValidKey(key) || version == "" || len(version) > 1024 || strings.EqualFold(version, "null") || strings.ContainsAny(version, "\r\n\x00") {
		return ErrInvalid
	}
	verifier, ok := store.(VersionAbsenceVerifier)
	if !ok {
		return ErrUnavailable
	}
	if err := verifier.VerifyVersionAbsent(ctx, key, version); err != nil {
		return err
	}
	return ctx.Err()
}
