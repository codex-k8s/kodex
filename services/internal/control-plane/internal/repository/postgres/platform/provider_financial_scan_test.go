package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

type financialAccountScanFixture struct {
	raw      []byte
	received *time.Time
}

func (row financialAccountScanFixture) Scan(values ...any) error {
	if len(values) != 20 {
		return errors.New("financial account fixture shape is invalid")
	}
	*values[0].(*string) = "pac_fixture_financial"
	*values[1].(*string) = "openai-codex"
	*values[2].(*string) = "Synthetic account"
	*values[4].(*string) = "AUTHORIZED"
	*values[5].(*bool) = true
	*values[6].(*int64) = 1
	*values[17].(*int32) = 10
	*values[18].(*[]byte) = row.raw
	*values[19].(**time.Time) = row.received
	return nil
}

func TestFinancialAccountScanRejectsCorruptProjection(t *testing.T) {
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	raw := []byte(`{"sequence":1,"state":"AVAILABLE","windows":[{"usedPercent":100}],"credits":{"hasCredits":true,"balance":"59999.1234567890123456789"}}`)
	account, err := scanProviderAccount(financialAccountScanFixture{raw: raw, received: &at})
	if err != nil || account.FinancialSummary == nil || account.FinancialSummary.Observation.Credits == nil || *account.FinancialSummary.Observation.Credits.Balance != "59999.1234567890123456789" {
		t.Fatal("valid stored paid observation was not read")
	}
	for _, fixture := range []financialAccountScanFixture{
		{raw: raw},
		{raw: []byte(`{"sequence":1,"state":"AVAILABLE","accountId":"synthetic-private"}`), received: &at},
		{raw: []byte(`{"sequence":1,"sequence":2,"state":"AVAILABLE"}`), received: &at},
	} {
		account, err := scanProviderAccount(fixture)
		if !errors.Is(err, errs.ErrUnavailable) || account.FinancialSummary != nil {
			t.Fatal("corrupt stored observation was partially projected")
		}
	}
}
