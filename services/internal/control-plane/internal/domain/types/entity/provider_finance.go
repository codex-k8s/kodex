package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Проекция назначается владельцем account и не влияет на launch eligibility.
type ProviderFinancialSummary struct {
	Observation runtimecontract.ProviderFinancialObservation `json:"observation"`
	ReceivedAt  time.Time                                    `json:"receivedAt"`
	Version     int64                                        `json:"version"`
}

type ProviderFinancialDisplayAccount struct {
	Ref              string                    `json:"ref"`
	Name             string                    `json:"name"`
	FinancialSummary *ProviderFinancialSummary `json:"financialSummary,omitempty"`
}

type ProviderFinancialOverview struct {
	Accounts   []ProviderFinancialDisplayAccount `json:"accounts"`
	Total      int64                             `json:"total"`
	Exhausted  int64                             `json:"exhausted"`
	Unknown    int64                             `json:"unknown"`
	ObservedAt time.Time                         `json:"observedAt"`
	ValidUntil time.Time                         `json:"validUntil"`
}

// Полный scan хранит только три строки; Add вызывается после owner eligibility.
func (overview *ProviderFinancialOverview) Add(account ProviderFinancialDisplayAccount) {
	overview.Total++
	remaining, known := FinancialRemaining(account, overview.ObservedAt)
	if overview.ValidUntil.IsZero() {
		overview.ValidUntil = overview.ObservedAt.Add(5 * time.Minute)
	}
	if known && account.FinancialSummary.ReceivedAt.Add(5*time.Minute).Before(overview.ValidUntil) {
		overview.ValidUntil = account.FinancialSummary.ReceivedAt.Add(5 * time.Minute)
	}
	if !known {
		overview.Unknown++
	} else if remaining == 0 {
		overview.Exhausted++
	}
	overview.Accounts = append(overview.Accounts, account)
	for i := len(overview.Accounts) - 1; i > 0; i-- {
		a, ak := FinancialRemaining(overview.Accounts[i], overview.ObservedAt)
		b, bk := FinancialRemaining(overview.Accounts[i-1], overview.ObservedAt)
		if !ak {
			a = 101
		}
		if !bk {
			b = 101
		}
		if a > b || a == b && overview.Accounts[i].Ref >= overview.Accounts[i-1].Ref {
			break
		}
		overview.Accounts[i], overview.Accounts[i-1] = overview.Accounts[i-1], overview.Accounts[i]
	}
	if len(overview.Accounts) > 3 {
		overview.Accounts = overview.Accounts[:3]
	}
}

func FinancialRemaining(account ProviderFinancialDisplayAccount, at time.Time) (int32, bool) {
	summary := account.FinancialSummary
	if summary == nil || summary.Observation.State != "AVAILABLE" || summary.ReceivedAt.After(at.Add(30*time.Second)) || at.Sub(summary.ReceivedAt) > 5*time.Minute || len(summary.Observation.Windows) == 0 || summary.Observation.Validate() != nil {
		return 0, false
	}
	remaining := int32(100)
	for _, window := range summary.Observation.Windows {
		if value := 100 - *window.UsedPercent; value < remaining {
			remaining = value
		}
	}
	return remaining, true
}
