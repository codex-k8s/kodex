// Package managedssh defines the closed wire contract shared by the broker and its operator-installed SSH helper; it carries no credentials or grant authority.
// Package managedssh задаёт закрытый wire-контракт broker и устанавливаемого оператором SSH helper; credentials и grant authority в нём отсутствуют.
package managedssh

import "errors"

// ErrProtocol rejects unknown operations, malformed frames or incompatible helper replies without exposing request bytes or remote diagnostics.
// ErrProtocol отклоняет неизвестные операции, неверные frames либо несовместимые ответы без раскрытия request bytes и удалённой диагностики.
var ErrProtocol = errors.New("managed SSH protocol rejected")

// Operation identifies one built-in provider recipe; arbitrary shell execution uses a separate EACH-only broker path and is never encoded as a read recipe.
// Operation обозначает один встроенный provider recipe; произвольный shell использует отдельный EACH-only путь broker и не кодируется как read recipe.
type Operation uint8

const (
	// Inspect verifies the exact helper profile and nonprivileged identity without performing a managed change.
	// Inspect проверяет exact helper profile и непривилегированную identity без управляемого изменения.
	Inspect Operation = 1
	// SystemRead returns only fixed operating-system metadata through native reads, without shell or caller-selected programs.
	// SystemRead возвращает только фиксированные сведения ОС через native reads, без shell и выбранных caller программ.
	SystemRead Operation = 2
	// ProcessList returns bounded metadata for processes of the selected UID, excluding arguments and environment.
	// ProcessList возвращает bounded metadata процессов выбранного UID без аргументов и окружения.
	ProcessList Operation = 3
	// FileList lists a bounded page inside an operator-selected root; it does not select an arbitrary host directory.
	// FileList перечисляет bounded page внутри выбранного оператором root, а не произвольного host directory.
	FileList Operation = 4
	// FileStat inspects a selected root-relative entry without following symlinks outside the qualified filesystem contract.
	// FileStat проверяет выбранный root-relative entry без следования symlinks за пределы квалифицированного filesystem contract.
	FileStat Operation = 5
	// FileRead returns a bounded version-bound fragment of one selected regular file.
	// FileRead возвращает bounded version-bound фрагмент одного выбранного regular file.
	FileRead Operation = 6
	// FileCreate creates an absent managed file under the helper's enforced namespace and scoped consent.
	// FileCreate создаёт отсутствующий managed file внутри принудительно ограниченного namespace по scoped consent.
	FileCreate Operation = 7
	// FileReplace replaces a version-bound managed file only under a qualified exclusive-writer contract.
	// FileReplace заменяет version-bound managed file только при квалифицированном exclusive-writer contract.
	FileReplace Operation = 8
	// FileDelete deletes a version-bound managed entry and always requires a separate EACH decision at the owner boundary.
	// FileDelete удаляет version-bound managed entry и всегда требует отдельного EACH решения в owner boundary.
	FileDelete Operation = 9
	// ServiceRead reads fixed status fields of an operator-selected service without projecting its environment or command line.
	// ServiceRead читает фиксированные status fields выбранного оператором service без environment либо command line.
	ServiceRead Operation = 10
	// ServiceRestart restarts one operator-selected system service under a distinct UID and requires EACH consent for the delegated privilege.
	// ServiceRestart перезапускает выбранный оператором system service под отдельным UID и требует EACH consent для делегированной привилегии.
	ServiceRestart Operation = 11
	// ServiceReload reloads one operator-selected system service under EACH consent, without caller-selected executable or argv.
	// ServiceReload перезагружает выбранный оператором system service по EACH consent, без выбранных caller executable либо argv.
	ServiceReload Operation = 12
	// ReceiptRead observes one immutable helper receipt and never resends the original managed change.
	// ReceiptRead читает один immutable helper receipt и не отправляет исходное управляемое изменение повторно.
	ReceiptRead Operation = 13
	// JournalRead returns a bounded fragment of one operator-selected service journal, never a caller-selected filter or command.
	// JournalRead возвращает bounded fragment журнала одного выбранного оператором service, а не caller-selected filter либо command.
	JournalRead Operation = 14
)

// operationNames is a finite positional protocol registry; names are metadata rather than authorization sources.
// operationNames — конечный позиционный protocol registry; имена являются metadata, а не источником полномочий.
var operationNames = [...]string{"", "ssh.target.inspect", "ssh.system.read", "ssh.process.list", "ssh.file.list", "ssh.file.stat", "ssh.file.read", "ssh.file.create", "ssh.file.replace", "ssh.file.delete", "ssh.service.read", "ssh.service.restart", "ssh.service.reload", "ssh.receipt.read", "ssh.journal.read"}

// ParseOperation recognizes only the built-in managed recipe keys; prefixes, shell syntax and unsupported names cannot create a new operation.
// ParseOperation распознаёт только встроенные managed recipe keys; префиксы, shell syntax и неизвестные имена не создают новую операцию.
func ParseOperation(value string) (Operation, error) {
	for index := 1; index < len(operationNames); index++ {
		if operationNames[index] == value {
			return Operation(index), nil
		}
	}
	return 0, ErrProtocol
}

// Name returns the registered capability key or an empty string for an unknown wire value.
// Name возвращает зарегистрированный capability key либо пустую строку для неизвестного wire value.
func (operation Operation) Name() string {
	if operation == 0 || int(operation) >= len(operationNames) {
		return ""
	}
	return operationNames[operation]
}

// EffectClass describes the minimum owner decision for a known recipe; possession of this value is never a grant.
// EffectClass описывает минимальное owner решение для известного recipe; само значение никогда не является grant.
type EffectClass uint8

const (
	// ReadOnly describes the finite native read recipes qualified by the operator profile.
	// ReadOnly обозначает конечные native read recipes, квалифицированные operator profile.
	ReadOnly EffectClass = 1
	// ScopedChange describes managed changes bound to exact target, recipe, arguments and scoped consent limits.
	// ScopedChange обозначает managed changes, связанные с exact target, recipe, arguments и ограничениями scoped consent.
	ScopedChange EffectClass = 2
	// EachChange describes destructive changes requiring a fresh decision for every effect.
	// EachChange обозначает destructive changes, требующие свежего решения для каждого effect.
	EachChange EffectClass = 3
)

// Effect returns the closed classification only for a known operation; unknown wire values fail instead of becoming reads.
// Effect возвращает закрытую классификацию только для известной операции; неизвестные wire values отклоняются, а не становятся чтением.
func (operation Operation) Effect() (EffectClass, error) {
	switch operation {
	case Inspect, SystemRead, ProcessList, FileList, FileStat, FileRead, ServiceRead, ReceiptRead, JournalRead:
		return ReadOnly, nil
	case FileCreate, FileReplace:
		return ScopedChange, nil
	case FileDelete, ServiceRestart, ServiceReload:
		return EachChange, nil
	default:
		return 0, ErrProtocol
	}
}
