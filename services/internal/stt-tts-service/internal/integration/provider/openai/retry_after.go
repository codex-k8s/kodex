package openai

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sttapi/errorprofile"
)

// boundedRetryAfter accepts one short delta-seconds or HTTP-date hint within the shared retry budget.
// Future dates round up to seconds; invalid, repeated, expired, or excessive hints yield zero, never a retry action.
// boundedRetryAfter принимает одну короткую подсказку в секундах либо HTTP-дате в пределах общего бюджета повтора.
// Будущая дата округляется вверх; неверная, повторная, прошедшая или чрезмерная подсказка даёт ноль, не действие повтора.
func boundedRetryAfter(header http.Header, now time.Time) time.Duration {
	values := header.Values("Retry-After")
	if len(values) != 1 || len(values[0]) > 128 {
		return 0
	}
	text := strings.TrimSpace(values[0])
	if text == "" {
		return 0
	}
	digits := true
	for _, value := range text {
		if value < '0' || value > '9' {
			digits = false
			break
		}
	}
	var delay time.Duration
	if digits {
		seconds, err := strconv.ParseUint(text, 10, 64)
		if err != nil || seconds < 1 || seconds > uint64(errorprofile.MaximumRetryAfter/time.Second) {
			return 0
		}
		delay = time.Duration(seconds) * time.Second
	} else {
		deadline, err := http.ParseTime(text)
		if err != nil {
			return 0
		}
		delta := deadline.Sub(now)
		if delta <= 0 || delta > errorprofile.MaximumRetryAfter {
			return 0
		}
		delay = ((delta + time.Second - 1) / time.Second) * time.Second
	}
	if delay < time.Second || delay > errorprofile.MaximumRetryAfter {
		return 0
	}
	return delay
}
