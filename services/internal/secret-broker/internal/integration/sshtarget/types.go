package sshtarget

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// ErrRejected identifies a failure before command dispatch without exposing credentials, commands or remote diagnostics.
// ErrRejected обозначает отказ до отправки команды без раскрытия credentials, команды либо удалённой диагностики.
var ErrRejected = errors.New("SSH operation rejected")

// ErrUnknown preserves an ambiguous post-dispatch outcome; callers must not resend the command during recovery.
// ErrUnknown сохраняет неоднозначный исход после отправки; recovery не должен повторно отправлять команду.
var ErrUnknown = errors.New("SSH operation outcome unknown")

// ErrOutputLimit identifies a bounded capture overflow while remaining private behind the transport's safe failure boundary.
// ErrOutputLimit обозначает переполнение ограниченного output capture, оставаясь за безопасной границей transport failure.
var ErrOutputLimit = errors.New("SSH output limit exceeded")

// userPattern admits one literal nonprivileged account name; its observed UID and authority remain owner/operator responsibilities.
// userPattern допускает одно буквальное имя непривилегированной учётной записи; наблюдаемые UID и полномочия принадлежат owner/operator.
var userPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Target binds transport to one owner-selected literal endpoint/account and exactly one canonical public key or trusted SHA256 host pin; the private DTO grants no authority by itself.
// Target связывает transport с owner-selected literal endpoint/account и ровно одним canonical public key либо trusted SHA256 host pin; private DTO сам не выдаёт authority.
type Target struct {
	Address          string
	Port             uint16
	User             string
	HostKey          string
	HostKeySHA256    string
	HostKeyAlgorithm managedssh.HostKeyAlgorithm
}

// Validate rejects ambiguous network destinations and obvious privileged account names before dialing; private routed addresses require explicit owner/operator selection and exact egress.
// Validate до подключения отклоняет неоднозначные назначения и очевидные привилегированные account names; private routed addresses требуют явного owner/operator выбора и exact egress.
func (target Target) Validate() error {
	address, err := netip.ParseAddr(target.Address)
	if err != nil || address.String() != target.Address || address.Is4In6() || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() || target.Port == 0 ||
		!userPattern.MatchString(target.User) || target.User == "root" || len(target.HostKey) > 16384 || strings.TrimSpace(target.HostKey) != target.HostKey {
		return ErrRejected
	}
	_, err = target.hostPin()
	return err
}

// Command is an already authorized exact exec request, never a READ classifier or a public credential-bearing API; upper layers resolve its immutable digest and gate.
// Command — уже разрешённый exact exec request, а не READ classifier или публичный credential-bearing API; верхние слои разрешают immutable digest и gate.
type Command struct {
	Text          string
	Input         []byte
	MaximumOutput int
}

// Result reports bounded SSH session evidence only; an exit status does not prove that remote descendants stopped or that an arbitrary command's intended effect succeeded.
// Result содержит только bounded SSH session evidence; exit status не доказывает остановку remote descendants либо успех предполагаемого эффекта произвольной команды.
type Result struct {
	Stdout        []byte
	Stderr        []byte
	ExitStatus    int
	SessionExited bool
	Truncated     bool
}

// Failure retains a private SDK cause for classification while keeping its public diagnostic fixed and its dispatch ambiguity explicit.
// Failure сохраняет приватную SDK cause для классификации, оставляя публичную диагностику фиксированной и явно отражая неоднозначность dispatch.
type Failure struct {
	cause   error
	unknown bool
}

// Error returns only a stable English diagnostic, including when the SDK error embeds a sensitive command or key parser detail.
// Error возвращает только стабильную English диагностику, в том числе когда SDK error содержит чувствительную команду или key parser detail.
func (failure *Failure) Error() string {
	if failure.unknown {
		return ErrUnknown.Error()
	}
	return ErrRejected.Error()
}

// Is preserves exactly one outcome identity while retaining other private SDK/context classifications; a post-dispatch cause cannot downgrade UNKNOWN into a known rejection.
// Is сохраняет ровно одну outcome identity и остальные приватные SDK/context classifications; cause после dispatch не может превратить UNKNOWN в известный отказ.
func (failure *Failure) Is(target error) bool {
	if target == ErrUnknown {
		return failure.unknown
	}
	if target == ErrRejected {
		return !failure.unknown
	}
	return errors.Is(failure.cause, target)
}

// As retains typed SDK cause classification without exposing it through an ordinary error string or allowing its sentinel chain to override the committed outcome class.
// As сохраняет typed SDK cause classification без раскрытия в обычной error string и без переопределения committed outcome class вложенной sentinel chain.
func (failure *Failure) As(target any) bool { return errors.As(failure.cause, target) }
