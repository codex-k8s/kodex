package sshtarget

import (
	"slices"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/crypto/ssh"
)

const (
	// numericUIDCommand is a fixed operator-qualified read probe; employees cannot choose its executable, arguments, stdin or output budget.
	// numericUIDCommand задаёт fixed operator-qualified read probe; сотрудник не выбирает executable, arguments, stdin либо output budget.
	numericUIDCommand = "/usr/bin/id -u"
	// numericRealUIDCommand rejects an account that retained a root or changed real UID despite an apparently unprivileged effective UID.
	// numericRealUIDCommand отклоняет account с retained root либо changed real UID при внешне непривилегированном effective UID.
	numericRealUIDCommand = "/usr/bin/id -r -u"
	// numericGIDCommand observes the effective group through the same authenticated SSH connection before any requested effect.
	// numericGIDCommand наблюдает effective group через тот же authenticated SSH connection до любого запрошенного effect.
	numericGIDCommand = "/usr/bin/id -g"
	// numericRealGIDCommand binds the actual real group as well as the effective group to the selected non-root identity.
	// numericRealGIDCommand связывает actual real group вместе с effective group с выбранной non-root identity.
	numericRealGIDCommand = "/usr/bin/id -r -g"
	// numericGroupsCommand observes the full numeric group set, including the primary group, under the operator's pinned id executable lifecycle.
	// numericGroupsCommand наблюдает полный numeric group set, включая primary group, при operator-pinned lifecycle executable id.
	numericGroupsCommand = "/usr/bin/id -G"
)

// freshNumericIdentity uses only fixed bounded reads and rejects zero, changed or malformed identities before sending the business command; SSH host/root deployment remains the trusted observer.
// freshNumericIdentity использует только fixed bounded reads и отклоняет zero, changed либо malformed identity до business command; SSH host/root deployment остаётся доверенным наблюдателем.
func freshNumericIdentity(remote *ssh.Client, closeConnection func() error, expected managedssh.TargetQualification) error {
	for _, probe := range []struct {
		command string
		values  []uint32
	}{
		{numericUIDCommand, []uint32{expected.UID}},
		{numericRealUIDCommand, []uint32{expected.UID}},
		{numericGIDCommand, []uint32{expected.GID}},
		{numericRealGIDCommand, []uint32{expected.GID}},
		{numericGroupsCommand, expected.Groups},
	} {
		output, err := numericIdentityRead(remote, closeConnection, probe.command)
		if err != nil {
			return ErrRejected
		}
		values, err := numericIdentityValues(output)
		if err != nil || !slices.Equal(values, probe.values) {
			return ErrRejected
		}
	}
	return nil
}

// numericIdentityRead joins one ordinary SSH read session within the outer operation deadline, rejecting stderr, nonzero/missing exit status and response overflow without exposing peer diagnostics.
// numericIdentityRead завершает одну ordinary SSH read session в outer operation deadline, отклоняя stderr, nonzero/missing exit status и response overflow без peer diagnostics.
func numericIdentityRead(remote *ssh.Client, closeConnection func() error, command string) ([]byte, error) {
	session, err := remote.NewSession()
	if err != nil {
		return nil, ErrRejected
	}
	defer session.Close()
	capture := &outputCapture{maximum: 1024, close: closeConnection}
	session.Stdout, session.Stderr = captureWriter{capture: capture}, captureWriter{capture: capture, stderr: true}
	if session.Run(command) != nil {
		return nil, ErrRejected
	}
	result := capture.result()
	if result.Truncated || len(result.Stderr) != 0 {
		return nil, ErrRejected
	}
	return result.Stdout, nil
}

// numericIdentityValues parses the exact numeric id output without names or locale coercion, retaining at most 32 distinct non-root groups and canonical nonzero uint32 values.
// numericIdentityValues разбирает exact numeric id output без имён либо locale coercion, сохраняя не более32 distinct non-root groups и canonical nonzero uint32 values.
func numericIdentityValues(raw []byte) ([]uint32, error) {
	if len(raw) < 2 || len(raw) > 1024 || raw[len(raw)-1] != '\n' {
		return nil, ErrRejected
	}
	fields := strings.Split(string(raw[:len(raw)-1]), " ")
	if len(fields) == 0 || len(fields) > 32 {
		return nil, ErrRejected
	}
	values := make([]uint32, len(fields))
	for index, field := range fields {
		number, err := strconv.ParseUint(field, 10, 32)
		if err != nil || number == 0 || strconv.FormatUint(number, 10) != field {
			return nil, ErrRejected
		}
		values[index] = uint32(number)
	}
	slices.Sort(values)
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return nil, ErrRejected
		}
	}
	return values, nil
}
