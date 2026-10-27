package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

// acceptFinancialCollector compares collector creation time/lease and same-lease sequence after
// caller-owned authority/account locking. Equal lease/sequence requires decoded JSON equality and
// is a no-op; older ordering is a no-op, otherwise accepted. Missing previous time accepts
// immediately; this pure predicate performs no persistence or authority checks.
// acceptFinancialCollector сравнивает collector creation time/lease и same-lease sequence после
// caller-owned authority/account lock. Equal lease/sequence требует decoded JSON equality и даёт
// no-op; старый порядок даёт no-op, прочее принимается. Отсутствующий previous time сразу
// принимается; pure predicate не записывает данные и не проверяет authority.
func acceptFinancialCollector(lease string, created time.Time, sequence int64, body []byte, previousLease string, previousCreated *time.Time, previousSequence int64, previousBody []byte) (bool, error) {
	if previousCreated == nil {
		return true, nil
	}
	if lease == previousLease && sequence == previousSequence {
		var current, previous any
		if json.Unmarshal(body, &current) != nil || json.Unmarshal(previousBody, &previous) != nil || !reflect.DeepEqual(current, previous) {
			return false, errors.New("financial observation sequence content mismatch")
		}
		return false, nil
	}
	if lease == previousLease && sequence < previousSequence || created.Before(*previousCreated) || created.Equal(*previousCreated) && lease < previousLease {
		return false, nil
	}
	return true, nil
}
