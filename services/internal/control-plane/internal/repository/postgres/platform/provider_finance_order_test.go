package platform

import (
	"testing"
	"time"
)

func TestFinancialCollectorConcurrencyAndReplay(t *testing.T) {
	at := time.Unix(100, 0)
	old := at.Add(-time.Second)
	later := at.Add(time.Second)
	previous := []byte(`{"sequence":2,"credits":{"balance":"59999.123456789"}}`)
	equivalent := []byte(`{ "credits": {"balance":"59999.123456789"}, "sequence":2 }`)
	changed := []byte(`{"sequence":2,"credits":{"balance":"0"}}`)
	tests := []struct {
		name, lease string
		at          time.Time
		seq         int64
		body        []byte
		want        bool
		failure     bool
	}{
		{"duplicate same body", "a", at, 2, equivalent, false, false},
		{"sequence content mismatch", "a", at, 2, changed, false, true},
		{"old same collector", "a", at, 1, previous, false, false},
		{"next same collector", "a", at, 3, previous, true, false},
		{"old concurrent collector", "b", old, 40, previous, false, false},
		{"new concurrent collector", "b", later, 1, previous, true, false},
		{"server timestamp tie", "b", at, 1, previous, true, false},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			accepted, err := acceptFinancialCollector(item.lease, item.at, item.seq, item.body, "a", &at, 2, previous)
			if accepted != item.want || (err != nil) != item.failure {
				t.Fatalf("accept=%t error=%v", accepted, err)
			}
		})
	}
	if accepted, err := acceptFinancialCollector("a", at, 1, previous, "", nil, 0, nil); !accepted || err != nil {
		t.Fatal("first observation rejected")
	}
}
