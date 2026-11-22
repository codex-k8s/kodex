package managedssh

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Status is evidence of a fixed helper outcome, never evidence that an arbitrary remote shell or descendant process joined.
// Status обозначает evidence фиксированного helper outcome, а не завершение произвольного remote shell либо дочернего процесса.
type Status string

const (
	// Observed identifies a successful bounded read without managed changes.
	// Observed обозначает успешное bounded read без управляемых изменений.
	Observed Status = "OBSERVED"
	// Applied identifies a managed effect with a durable completed helper receipt.
	// Applied обозначает managed effect с durable completed helper receipt.
	Applied Status = "APPLIED"
	// Rejected identifies a known refusal without dispatching the selected managed effect.
	// Rejected обозначает известный отказ без отправки выбранного managed effect.
	Rejected Status = "REJECTED"
	// Unknown retains an intent whose managed effect cannot be authoritatively resolved; it is never a retry instruction.
	// Unknown сохраняет intent, effect которого невозможно авторитетно определить; это не инструкция для retry.
	Unknown Status = "UNKNOWN"
)

// Identity describes the actual nonprivileged helper process and executable, allowing the broker to compare operator-qualified pins.
// Identity описывает фактический nonprivileged helper process и executable для сравнения broker с operator-qualified pins.
type Identity struct {
	UID        uint32   `json:"uid"`
	GID        uint32   `json:"gid"`
	Groups     []uint32 `json:"groups"`
	Executable string   `json:"executable_sha256"`
}

// System contains fixed kernel metadata only; hostname, environment, network configuration and arbitrary files are not projected.
// System содержит только фиксированные kernel metadata; hostname, environment, network configuration и произвольные files не проецируются.
type System struct {
	Name         string `json:"name"`
	Release      string `json:"release"`
	Architecture string `json:"architecture"`
}

// Process projects one process belonging to the helper UID, excluding argv, environment, working directory and open descriptors.
// Process проецирует один процесс helper UID без argv, environment, working directory и open descriptors.
type Process struct {
	PID  uint32 `json:"pid"`
	Name string `json:"name"`
}

// Entry describes a single direct child name; symlinks and special files may be listed as blocked without reading their targets.
// Entry описывает одно непосредственное дочернее имя; symlinks и special files могут перечисляться как blocked без чтения их targets.
type Entry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// File describes one regular single-link file inside an enforced root; its digest is the complete bounded content version.
// File описывает один regular single-link file внутри принудительного root; digest является версией полного bounded content.
type File struct {
	Size     uint64 `json:"size"`
	Digest   string `json:"sha256"`
	Modified int64  `json:"modified_unix_nano"`
}

// Fragment carries one version-bound binary fragment with explicit offset and truncation; consumers treat all bytes as untrusted content.
// Fragment содержит один version-bound binary fragment с явными offset и truncation; consumers рассматривают bytes как untrusted content.
type Fragment struct {
	File      File   `json:"file"`
	Offset    uint64 `json:"offset"`
	Content   []byte `json:"content"`
	Truncated bool   `json:"truncated"`
}

// Service projects only fixed state of a selected system unit; unit files, command lines and environment remain private.
// Service проецирует только фиксированное состояние выбранного system unit; unit files, command lines и environment остаются приватными.
type Service struct {
	State string `json:"state"`
}

// Log projects one bounded journal message and timestamp without journal metadata, credentials or caller-selected fields.
// Log проецирует одно bounded journal message и timestamp без journal metadata, credentials либо caller-selected fields.
type Log struct {
	Microseconds uint64 `json:"microseconds"`
	Message      string `json:"message"`
}

// Receipt identifies exactly one durable managed attempt; a preexisting intent without completion remains UNKNOWN and cannot authorize replay.
// Receipt обозначает ровно одну durable managed attempt; существующий intent без completion остаётся UNKNOWN и не разрешает replay.
type Receipt struct {
	AttemptID     string    `json:"attempt_id"`
	RequestDigest string    `json:"request_digest"`
	Operation     Operation `json:"operation"`
	Status        Status    `json:"status"`
	Version       string    `json:"version,omitempty"`
}

// Reply is a closed result union; error codes are fixed and remote diagnostics never become API errors or executable instructions.
// Reply является closed result union; error codes фиксированы, remote diagnostics не становятся API errors либо executable instructions.
type Reply struct {
	Version       uint8     `json:"version"`
	ProfileDigest string    `json:"profile_digest"`
	RequestDigest string    `json:"request_digest"`
	Operation     Operation `json:"operation"`
	Status        Status    `json:"status"`
	Code          string    `json:"code,omitempty"`
	Identity      *Identity `json:"identity,omitempty"`
	System        *System   `json:"system,omitempty"`
	Processes     []Process `json:"processes,omitempty"`
	Entries       []Entry   `json:"entries,omitempty"`
	File          *File     `json:"file,omitempty"`
	Fragment      *Fragment `json:"fragment,omitempty"`
	Service       *Service  `json:"service,omitempty"`
	Logs          []Log     `json:"logs,omitempty"`
	Receipt       *Receipt  `json:"receipt,omitempty"`
	Truncated     bool      `json:"truncated,omitempty"`
}

// EncodeReply validates the result against its original request before encoding; it cannot produce a reply for another profile or operation.
// EncodeReply до сериализации проверяет result по исходному request; ответ для другого profile либо operation не создаётся.
func EncodeReply(request Request, reply Reply) ([]byte, error) {
	if reply.validate(request) != nil {
		return nil, ErrProtocol
	}
	raw, err := canonical(reply)
	if err != nil || len(raw) > MaximumReply {
		return nil, ErrProtocol
	}
	return raw, nil
}

// EncodeBoundedReply validates the complete semantic result before limiting read collections at record boundaries; writes, receipts and exact file fragments are never shortened or reinterpreted.
// EncodeBoundedReply до ограничения read collections по границам records проверяет полную семантику result; writes, receipts и exact file fragments не укорачиваются и не переосмысляются.
func EncodeBoundedReply(request Request, reply Reply) ([]byte, error) {
	if reply.validate(request) != nil {
		return nil, ErrProtocol
	}
	for {
		raw, err := json.Marshal(reply)
		if err != nil {
			return nil, ErrProtocol
		}
		if len(raw) <= MaximumReply {
			return raw, nil
		}
		switch request.Operation {
		case ProcessList:
			if len(reply.Processes) == 0 {
				return nil, ErrProtocol
			}
			reply.Processes = reply.Processes[:len(reply.Processes)-1]
		case FileList:
			if len(reply.Entries) == 0 {
				return nil, ErrProtocol
			}
			reply.Entries = reply.Entries[:len(reply.Entries)-1]
		case JournalRead:
			if len(reply.Logs) == 0 {
				return nil, ErrProtocol
			}
			reply.Logs = reply.Logs[1:]
		default:
			return nil, ErrProtocol
		}
		reply.Truncated = true
	}
}

// DecodeReply rejects malformed or noncanonical data and checks every pin and operation-specific payload before a broker consumer uses it.
// DecodeReply отклоняет malformed либо noncanonical data и проверяет каждый pin и operation-specific payload до использования broker consumer.
func DecodeReply(request Request, raw []byte) (Reply, error) {
	var reply Reply
	if len(raw) > MaximumReply || decodeCanonical(raw, &reply) != nil || reply.validate(request) != nil {
		return Reply{}, ErrProtocol
	}
	return reply, nil
}

// NewReply initializes exact request pins without claiming success, identity, permission or a completed effect.
// NewReply назначает exact request pins без утверждения success, identity, permission либо completed effect.
func NewReply(request Request, status Status) (Reply, error) {
	digest, err := RequestDigest(request)
	if err != nil {
		return Reply{}, err
	}
	return Reply{Version: Version, ProfileDigest: request.ProfileDigest, RequestDigest: digest, Operation: request.Operation, Status: status}, nil
}

// validate checks the whole result union and scalar bounds, including required nested fields and exact managed receipt binding.
// validate проверяет весь result union и scalar bounds, включая required nested fields и exact managed receipt binding.
func (reply Reply) validate(request Request) error {
	digest, err := RequestDigest(request)
	if err != nil || reply.Version != Version || reply.ProfileDigest != request.ProfileDigest || reply.RequestDigest != digest || reply.Operation != request.Operation {
		return ErrProtocol
	}
	count := 0
	for _, present := range []bool{reply.Identity != nil, reply.System != nil, len(reply.Processes) != 0, len(reply.Entries) != 0, reply.File != nil, reply.Fragment != nil, reply.Service != nil, len(reply.Logs) != 0, reply.Receipt != nil} {
		if present {
			count++
		}
	}
	if reply.Status == Rejected {
		if count != 0 || reply.Truncated || !rejectionCode(reply.Code) {
			return ErrProtocol
		}
		return nil
	}
	if reply.Code != "" || reply.Status != Observed && reply.Status != Applied && reply.Status != Unknown {
		return ErrProtocol
	}
	effect, _ := request.Operation.Effect()
	if effect != ReadOnly || request.Operation == ReceiptRead {
		if count != 1 || reply.Receipt == nil || reply.Truncated || reply.Receipt.validate() != nil || reply.Receipt.AttemptID != request.AttemptID {
			return ErrProtocol
		}
		if request.Operation != ReceiptRead && (reply.Receipt.RequestDigest != digest || reply.Receipt.Operation != request.Operation || reply.Status != reply.Receipt.Status) {
			return ErrProtocol
		}
		if request.Operation == ReceiptRead && reply.Status != Observed {
			return ErrProtocol
		}
		return nil
	}
	if reply.Status != Observed || count > 1 {
		return ErrProtocol
	}
	valid := false
	switch request.Operation {
	case Inspect:
		valid = count == 1 && reply.Identity != nil && reply.Identity.valid() && !reply.Truncated
	case SystemRead:
		valid = count == 1 && reply.System != nil && text(reply.System.Name, 64, false) && text(reply.System.Release, 128, false) && text(reply.System.Architecture, 64, false) && !reply.Truncated
	case ProcessList:
		valid = count == 0 || count == 1 && len(reply.Processes) != 0
		valid = valid && len(reply.Processes) <= int(request.Limit)
		previous := uint32(0)
		for _, process := range reply.Processes {
			valid = valid && process.PID > previous && text(process.Name, 64, false)
			previous = process.PID
		}
	case FileList:
		valid = count == 0 || count == 1 && len(reply.Entries) != 0
		valid = valid && len(reply.Entries) <= int(request.Limit)
		previous := ""
		for _, entry := range reply.Entries {
			valid = valid && RelativePath(entry.Name) && !strings.Contains(entry.Name, "/") && entry.Name > previous && (entry.Kind == "FILE" || entry.Kind == "DIRECTORY" || entry.Kind == "BLOCKED")
			previous = entry.Name
		}
	case FileStat:
		valid = count == 1 && reply.File != nil && reply.File.valid() && !reply.Truncated
	case FileRead:
		if count == 1 && reply.Fragment != nil {
			fragment := reply.Fragment
			valid = fragment.File.valid() && fragment.File.Digest == request.Expected && fragment.Offset == request.Offset && fragment.Offset <= fragment.File.Size && len(fragment.Content) <= int(request.Limit) && uint64(len(fragment.Content)) <= fragment.File.Size-fragment.Offset && fragment.Truncated == (uint64(len(fragment.Content)) < fragment.File.Size-fragment.Offset) && !reply.Truncated
			if valid {
				valid = uint64(len(fragment.Content)) == min(uint64(request.Limit), fragment.File.Size-fragment.Offset)
			}
		}
	case ServiceRead:
		valid = count == 1 && reply.Service != nil && !reply.Truncated
		if valid {
			switch reply.Service.State {
			case "ACTIVE", "INACTIVE", "FAILED", "ACTIVATING", "DEACTIVATING", "RELOADING", "UNKNOWN":
			default:
				valid = false
			}
		}
	case JournalRead:
		valid = count == 0 || count == 1 && len(reply.Logs) != 0
		valid = valid && len(reply.Logs) <= int(request.Limit)
		bytes := 0
		previous := uint64(0)
		for _, entry := range reply.Logs {
			valid = valid && entry.Microseconds > 0 && entry.Microseconds >= previous && text(entry.Message, 4096, true)
			previous = entry.Microseconds
			bytes += len(entry.Message)
		}
		valid = valid && bytes <= MaximumContent
	}
	if !valid {
		return ErrProtocol
	}
	return nil
}

// valid requires a complete bounded regular-file version without interpreting its digest as resource authority.
// valid требует полную bounded regular-file version, не рассматривая digest как resource authority.
func (file File) valid() bool {
	return file.Size <= 1<<20 && Digest(file.Digest) && file.Modified > 0
}

// valid forbids root identity and duplicate or unordered groups; the broker must still compare the exact approved UID/GID/group set.
// valid запрещает root identity и duplicate либо unordered groups; broker отдельно сравнивает exact approved UID/GID/group set.
func (identity Identity) valid() bool {
	if identity.UID == 0 || identity.GID == 0 || !Digest(identity.Executable) || len(identity.Groups) > 32 {
		return false
	}
	previous := uint32(0)
	for _, group := range identity.Groups {
		if group <= previous {
			return false
		}
		previous = group
	}
	return true
}

// validate admits managed operation receipts only, preserving UNKNOWN instead of guessing an effect from current file/service state.
// validate допускает receipts только managed operations, сохраняя UNKNOWN вместо догадки об effect по текущему file/service state.
func (receipt Receipt) validate() error {
	effect, err := receipt.Operation.Effect()
	if err != nil || effect == ReadOnly || !attemptID(receipt.AttemptID) || !Digest(receipt.RequestDigest) || receipt.Status != Applied && receipt.Status != Unknown || receipt.Version != "" && !Digest(receipt.Version) {
		return ErrProtocol
	}
	return nil
}

// rejectionCode admits fixed safe diagnostics only; provider text, paths and credential parser errors cannot be returned as a code.
// rejectionCode допускает только fixed safe diagnostics; provider text, paths и credential parser errors не могут возвращаться как code.
func rejectionCode(code string) bool {
	switch code {
	case "PROFILE_MISMATCH", "PROFILE_EXPIRED", "SCOPE_UNAVAILABLE", "NOT_FOUND", "VERSION_MISMATCH", "ALREADY_EXISTS", "ATTEMPT_CONFLICT", "RECIPE_REJECTED", "RESULT_LIMIT":
		return true
	default:
		return false
	}
}

// text bounds untrusted display data without accepting terminal control characters; journal messages may retain newline and tab only.
// text ограничивает untrusted display data без terminal control characters; journal messages могут сохранять только newline и tab.
func text(value string, maximum int, multiline bool) bool {
	if len(value) == 0 || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && !(multiline && (character == '\n' || character == '\t')) {
			return false
		}
	}
	return true
}
