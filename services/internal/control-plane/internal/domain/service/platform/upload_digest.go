package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// hashUploadContent seeks to the beginning, hashes at most maximumBytes+1 bytes and requires the exact declared length and any supplied sha256-prefixed digest. All seek/read/length/digest failures retain ErrInvalid. Callers must first validate the nonnil reader and bounded size/maximum; this helper neither authorizes the upload nor inspects its format, closes it or rewinds it after hashing.
// hashUploadContent возвращает позицию к началу, хеширует не более maximumBytes+1 байт и требует точную заявленную длину и переданный дайджест с префиксом sha256. Все ошибки seek/read/длины/дайджеста сохраняют ErrInvalid. Вызывающий заранее проверяет ненулевой reader и ограниченные размер и максимум; helper не авторизует загрузку, не исследует формат, не закрывает поток и не возвращает позицию к началу после хеширования.
func hashUploadContent(reader io.ReadSeeker, sizeBytes, maximumBytes int64, expectedDigest string) (string, error) {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return "", errs.ErrInvalid
	}
	contentDigest := sha256.New()
	written, err := io.Copy(contentDigest, io.LimitReader(reader, maximumBytes+1))
	if err != nil || written != sizeBytes {
		return "", errs.ErrInvalid
	}
	actualDigest := "sha256:" + hex.EncodeToString(contentDigest.Sum(nil))
	if expectedDigest != "" && expectedDigest != actualDigest {
		return "", errs.ErrInvalid
	}
	return actualDigest, nil
}
