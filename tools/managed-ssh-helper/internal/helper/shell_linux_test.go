package helper

import (
	"errors"
	"testing"
)

// TestManagedAccountShellRejectsCommandExtensions checks every non-fixed argv form before installed configuration, input or execution can be reached; shell syntax is never parsed for safety.
// TestManagedAccountShellRejectsCommandExtensions проверяет все non-fixed argv forms до installed configuration, input либо execution; shell syntax не разбирается для угадывания безопасности.
func TestManagedAccountShellRejectsCommandExtensions(t *testing.T) {
	for _, arguments := range [][]string{
		nil,
		{"-l"},
		{"-c", "id"},
		{"-c", installedHelperExecutable + "; id"},
		{"-c", installedHelperExecutable + " --read"},
		{"-c", "exec " + installedHelperExecutable},
		{"-c", installedHelperExecutable + "\n"},
		{"-c", installedHelperExecutable, "extra"},
		{"-c", "/usr/local/libexec/../libexec/kodex-ssh-target"},
		{"-c", "internal-sftp"},
	} {
		if err := ExecManagedAccountShell(t.Context(), arguments); !errors.Is(err, ErrRejected) {
			t.Fatal("managed account shell accepted a command extension")
		}
	}
}
