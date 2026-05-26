package service

import "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"

// BacklogOperationInput identifies a contract operation that reached the skeleton.
type BacklogOperationInput struct {
	Operation enum.Operation
}
