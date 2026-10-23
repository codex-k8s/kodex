package serviceruntime

import "time"

// IdleBackoff doubles idle polling delays up to a ceiling and resets on actual
// work. Next mutates state without synchronization; its owner serializes calls.
// IdleBackoff удваивает задержку пустого polling до предела и сбрасывает её
// после работы. Next меняет состояние без синхронизации; вызовы сериализует владелец.
type IdleBackoff struct {
	minimum time.Duration
	maximum time.Duration
	current time.Duration
}

// NewIdleBackoff normalizes nonpositive minimum to one millisecond and raises a
// smaller maximum to that minimum. Initial current equals minimum, so the first
// idle Next call doubles it unless already capped.
// NewIdleBackoff заменяет неположительный minimum одной миллисекундой и поднимает
// меньший maximum до minimum. Начальный current равен minimum, поэтому первый
// пустой Next удваивает его, если предел ещё не достигнут.
func NewIdleBackoff(minimum, maximum time.Duration) *IdleBackoff {
	if minimum <= 0 {
		minimum = time.Millisecond
	}
	if maximum < minimum {
		maximum = minimum
	}
	return &IdleBackoff{minimum: minimum, maximum: maximum, current: minimum}
}

// Next resets to minimum after work; otherwise it doubles the current delay and
// saturates at maximum, including when duration multiplication overflows.
// Next сбрасывает задержку к minimum после работы; иначе удваивает её и ограничивает
// maximum, в том числе при переполнении умножения time.Duration.
func (backoff *IdleBackoff) Next(didWork bool) time.Duration {
	if didWork {
		backoff.current = backoff.minimum
		return backoff.current
	}
	if backoff.current >= backoff.maximum {
		return backoff.maximum
	}
	next := backoff.current * 2
	if next < backoff.current || next > backoff.maximum {
		next = backoff.maximum
	}
	backoff.current = next
	return backoff.current
}
