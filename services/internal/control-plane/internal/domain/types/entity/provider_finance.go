package entity

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"time"
)

// Проекция назначается владельцем account и не влияет на launch eligibility.
type ProviderFinancialSummary struct {
	Observation runtimecontract.ProviderFinancialObservation `json:"observation"`
	ReceivedAt  time.Time                                    `json:"receivedAt"`
	Version     int64                                        `json:"version"`
}
