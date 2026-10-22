package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// ProviderFinancialSummary is the account owner's versioned financial-observation projection with its
// receive time. It does not determine launch eligibility; quota windows and available paid credits
// remain distinct reported facts.
// ProviderFinancialSummary является версионированной проекцией финансового наблюдения владельца
// аккаунта со временем получения. Она не определяет допустимость запуска; окна квоты и доступные
// платные кредиты остаются разными сообщёнными фактами.
type ProviderFinancialSummary struct {
	Observation runtimecontract.ProviderFinancialObservation `json:"observation"`
	ReceivedAt  time.Time                                    `json:"receivedAt"`
	Version     int64                                        `json:"version"`
}

// ProviderFinancialDisplayAccount contains account identity and display name with an optional
// financial summary. Missing summary means an unknown observation, not confirmed zero quota or zero
// paid balance.
// ProviderFinancialDisplayAccount содержит личность и имя аккаунта с необязательной финансовой
// сводкой. Отсутствие сводки означает неизвестное наблюдение, а не подтверждённый ноль квоты или
// платного баланса.
type ProviderFinancialDisplayAccount struct {
	Ref              string                    `json:"ref"`
	Name             string                    `json:"name"`
	FinancialSummary *ProviderFinancialSummary `json:"financialSummary,omitempty"`
}

// ProviderFinancialOverview accumulates eligible-account counts and retains up to three display rows
// ranked by observed remaining quota, with deterministic reference ties. Exhausted counts observed
// zero window headroom, not absence of paid credits; timestamps bound this display snapshot.
// ProviderFinancialOverview накапливает количества допустимых аккаунтов и сохраняет до трёх строк,
// упорядоченных по наблюдаемому остатку квоты с разрешением равенства по ссылке. Exhausted считает
// наблюдаемый нулевой остаток окна, а не отсутствие платных кредитов; время ограничивает снимок
// представления.
type ProviderFinancialOverview struct {
	Accounts   []ProviderFinancialDisplayAccount `json:"accounts"`
	Total      int64                             `json:"total"`
	Exhausted  int64                             `json:"exhausted"`
	Unknown    int64                             `json:"unknown"`
	ObservedAt time.Time                         `json:"observedAt"`
	ValidUntil time.Time                         `json:"validUntil"`
}

// Add includes one already-eligible account in the full-set totals, ranks known minimum window
// headroom before unknown values, breaks ties by reference, and retains three display rows. It updates
// display validity using known observation freshness; it neither resolves owner eligibility nor blocks
// launches or merges credits into quota percentages.
// Add включает уже допустимый аккаунт в общие количества, ставит известный минимальный остаток окон
// перед неизвестными значениями, разрешает равенство ссылкой и сохраняет три строки. Срок
// представления учитывает свежесть известных наблюдений; функция не определяет допустимость владельца,
// не блокирует запуск и не смешивает кредиты с процентами квоты.
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

// FinancialRemaining returns the minimum remaining percentage across valid AVAILABLE windows received
// no more than five minutes before the supplied time or thirty seconds after it. Missing, stale,
// invalid, or empty observations return unknown; credits are not converted to percentages and this
// helper does not decide submission eligibility.
// FinancialRemaining возвращает минимальный оставшийся процент корректных AVAILABLE окон, полученных
// не более пяти минут до указанного времени или тридцати секунд после него. Отсутствующие, старые,
// неверные и пустые наблюдения дают неизвестный результат; кредиты не переводятся в проценты и функция
// не определяет допустимость отправки задачи.
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
