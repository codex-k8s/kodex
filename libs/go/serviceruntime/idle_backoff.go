package serviceruntime

import "time"

// IdleBackoff ограничивает частоту опроса пустой очереди и сразу возвращает
// минимальную задержку после появления работы.
type IdleBackoff struct {
	minimum time.Duration
	maximum time.Duration
	current time.Duration
}

func NewIdleBackoff(minimum, maximum time.Duration) *IdleBackoff {
	if minimum <= 0 {
		minimum = time.Millisecond
	}
	if maximum < minimum {
		maximum = minimum
	}
	return &IdleBackoff{minimum: minimum, maximum: maximum, current: minimum}
}

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
