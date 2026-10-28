package runner

import "fmt"

// verificationFailure adds a closed verify stage while preserving repository sentinels and nil success.
// RU: verificationFailure добавляет закрытый этап verify, сохраняя ошибки репозитория и успешный nil.
func verificationFailure(stage string, err error) error {
	if err == nil {
		return nil
	}
	switch stage {
	case "lock_acquire", "manifest_load", "payload_verify", "receipt_ensure", "lock_release":
	default:
		stage = "unknown"
	}
	return fmt.Errorf("backup verification %s: %w", stage, err)
}
