package app

import (
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
)

// loadRestoreTargetApproval separates historical read identity from fresh cleanup authority using the closed command registry; unknown commands cannot opt into historical decoding.
// loadRestoreTargetApproval разделяет историческую идентичность чтения и действующее разрешение cleanup по закрытому реестру; неизвестная команда не может выбрать исторический decoder.
func loadRestoreTargetApproval(command, path string, now time.Time) (configspec.RestoreApproval, error) {
	switch command {
	case "inspect-restore-targets", "inspect-restore-cleanup":
		return configspec.LoadRestoreApprovalForInspection(path)
	case "cleanup-restore-drill":
		return configspec.LoadRestoreApproval(path, now)
	default:
		return configspec.RestoreApproval{}, errors.New("restore approval command is invalid")
	}
}
