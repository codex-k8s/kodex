package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

// Порядок сравнивается только после owner authority guard и account lock.
// Возвращаем false для валидного устаревшего наблюдения без повторного effect.
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
