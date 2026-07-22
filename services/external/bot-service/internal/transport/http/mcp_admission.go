package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
	"sync"
	"time"
)

type mcpCredentialBinding struct {
	sessionKeySHA256 [sha256.Size]byte
	tokenSHA256      [sha256.Size]byte
}

type mcpAdmittedTransport struct {
	binding    mcpCredentialBinding
	references int
	generation uint64
	closing    bool
	timer      *time.Timer
}

type mcpTransportAdmission struct {
	mu        sync.Mutex
	maximum   int
	timeout   time.Duration
	reserved  int
	active    map[string]*mcpAdmittedTransport
	closeLive func(string)
}

func newMCPTransportAdmission(maximum int, timeout time.Duration, closeLive func(string)) *mcpTransportAdmission {
	return &mcpTransportAdmission{
		maximum:   maximum,
		timeout:   timeout,
		active:    make(map[string]*mcpAdmittedTransport, maximum),
		closeLive: closeLive,
	}
}

func (admission *mcpTransportAdmission) reserve() bool {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if len(admission.active)+admission.reserved >= admission.maximum {
		return false
	}
	admission.reserved++
	return true
}

func (admission *mcpTransportAdmission) finishReservation(sessionID string, binding mcpCredentialBinding, live bool) *mcpAdmittedTransport {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.reserved > 0 {
		admission.reserved--
	}
	if !live || sessionID == "" {
		return nil
	}
	if _, exists := admission.active[sessionID]; exists {
		return nil
	}
	item := &mcpAdmittedTransport{binding: binding, references: 1}
	admission.active[sessionID] = item
	return item
}

func (admission *mcpTransportAdmission) begin(sessionID string, binding mcpCredentialBinding) (*mcpAdmittedTransport, bool) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	item := admission.active[sessionID]
	if item == nil || item.closing || !item.binding.equal(binding) {
		return nil, false
	}
	item.references++
	item.generation++
	if item.timer != nil {
		item.timer.Stop()
		item.timer = nil
	}
	return item, true
}

func (admission *mcpTransportAdmission) end(sessionID string, item *mcpAdmittedTransport, live bool) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.active[sessionID] != item {
		return
	}
	if item.references > 0 {
		item.references--
	}
	if !live {
		item.generation++
		if item.timer != nil {
			item.timer.Stop()
		}
		delete(admission.active, sessionID)
		return
	}
	if item.references == 0 {
		admission.scheduleLocked(sessionID, item)
	}
}

func (admission *mcpTransportAdmission) scheduleLocked(sessionID string, item *mcpAdmittedTransport) {
	item.generation++
	generation := item.generation
	item.timer = time.AfterFunc(admission.timeout, func() {
		admission.mu.Lock()
		if admission.active[sessionID] != item || item.generation != generation || item.references != 0 {
			admission.mu.Unlock()
			return
		}
		item.closing = true
		item.timer = nil
		admission.mu.Unlock()

		if admission.closeLive != nil {
			admission.closeLive(sessionID)
		}

		admission.mu.Lock()
		defer admission.mu.Unlock()
		if admission.active[sessionID] == item && item.closing && item.references == 0 {
			delete(admission.active, sessionID)
		}
	})
}

func (admission *mcpTransportAdmission) activeCount() int {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	return len(admission.active)
}

func (admission *mcpTransportAdmission) stateCount() int {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	return len(admission.active) + admission.reserved
}

func newMCPCredentialBinding(sessionKey string, token string) mcpCredentialBinding {
	return mcpCredentialBinding{
		sessionKeySHA256: sha256.Sum256([]byte(strings.TrimSpace(sessionKey))),
		tokenSHA256:      sha256.Sum256([]byte(strings.TrimSpace(token))),
	}
}

func (binding mcpCredentialBinding) equal(other mcpCredentialBinding) bool {
	return subtle.ConstantTimeCompare(binding.sessionKeySHA256[:], other.sessionKeySHA256[:]) == 1 &&
		subtle.ConstantTimeCompare(binding.tokenSHA256[:], other.tokenSHA256[:]) == 1
}
