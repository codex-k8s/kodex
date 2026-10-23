package serviceruntime

import (
	"testing"
	"time"
)

// TestIdleBackoff checks repeated doubling, ceiling saturation and reset after
// real work, including the first idle delay immediately after that reset.
// TestIdleBackoff проверяет удвоение, насыщение пределом и сброс после реальной
// работы, включая первую пустую задержку сразу после сброса.
func TestIdleBackoff(t *testing.T) {
	backoff := NewIdleBackoff(500*time.Millisecond, 5*time.Second)
	for index, expected := range []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		5 * time.Second,
		5 * time.Second,
	} {
		if actual := backoff.Next(false); actual != expected {
			t.Fatalf("idle delay %d: got %s, want %s", index, actual, expected)
		}
	}
	if actual := backoff.Next(true); actual != 500*time.Millisecond {
		t.Fatalf("active delay: got %s", actual)
	}
	if actual := backoff.Next(false); actual != time.Second {
		t.Fatalf("delay after reset: got %s", actual)
	}
}

// TestIdleBackoffNormalizesBounds keeps the delay at minimum when the configured
// maximum is smaller, rather than returning an out-of-range lower delay.
// TestIdleBackoffNormalizesBounds удерживает minimum при меньшем maximum,
// не возвращая слишком низкую задержку вне нормализованного диапазона.
func TestIdleBackoffNormalizesBounds(t *testing.T) {
	backoff := NewIdleBackoff(2*time.Second, time.Second)
	if actual := backoff.Next(false); actual != 2*time.Second {
		t.Fatalf("normalized delay: got %s", actual)
	}
}
