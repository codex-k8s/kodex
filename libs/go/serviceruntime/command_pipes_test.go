package serviceruntime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCommandPipesConnectsAllThreeStreams checks exact command assignments and pipe directions
// using real anonymous descriptors without starting any child process.
// TestCommandPipesConnectsAllThreeStreams сверяет назначения command и направления трёх каналов
// через настоящие анонимные дескрипторы без запуска дочернего процесса.
func TestCommandPipesConnectsAllThreeStreams(t *testing.T) {
	command := &exec.Cmd{}
	pipes, err := NewCommandPipes(command, "synthetic app-server")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipes.Close)
	if command.Stdin != pipes.StdinChild || command.Stdout != pipes.StdoutChild || command.Stderr != pipes.StderrChild || command.Process != nil {
		t.Fatal("command pipe assignment changed or a process was started")
	}
	for _, pair := range [][2]*os.File{{pipes.Stdin, pipes.StdinChild}, {pipes.StdoutChild, pipes.Stdout}, {pipes.StderrChild, pipes.Stderr}} {
		if _, err := pair[0].Write([]byte("x")); err != nil {
			t.Fatal("pipe write failed")
		}
		var body [1]byte
		if _, err := io.ReadFull(pair[1], body[:]); err != nil || body[0] != 'x' {
			t.Fatal("pipe direction or content changed")
		}
	}
}

// TestCommandPipesPartialFailuresReleaseEveryDescriptor injects failure at each allocation boundary,
// checking exact safe messages, no raw error disclosure, no output assignment and closure of prior ends.
// TestCommandPipesPartialFailuresReleaseEveryDescriptor подставляет отказ на каждой границе выделения,
// проверяя точные безопасные ошибки, отсутствие исходной причины, output assignments и утечки прежних концов.
func TestCommandPipesPartialFailuresReleaseEveryDescriptor(t *testing.T) {
	for failureAt, stream := range []string{"request", "response", "diagnostic"} {
		t.Run(stream, func(t *testing.T) {
			var acquired []*os.File
			calls := 0
			open := func() (*os.File, *os.File, error) {
				calls++
				if calls == failureAt+1 {
					return nil, nil, errors.New("private pipe error sentinel")
				}
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal("synthetic allocation setup failed")
				}
				acquired = append(acquired, reader, writer)
				t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
				return reader, writer, nil
			}
			command := &exec.Cmd{}
			pipes, err := newCommandPipes(command, "synthetic app-server", open)
			if err == nil || err.Error() != fmt.Sprintf("create synthetic app-server %s stream", stream) || strings.Contains(err.Error(), "sentinel") ||
				pipes != nil || calls != failureAt+1 || command.Stdout != nil || command.Stderr != nil || command.Process != nil {
				t.Fatal("failed allocation leaked data, retried or changed command ownership")
			}
			if (command.Stdin != nil) != (failureAt > 0) {
				t.Fatal("stdin assignment ordering changed")
			}
			assertCommandPipeFilesClosed(t, acquired)
		})
	}
}

// TestCommandPipesCloseReleasesCompleteAllocation verifies all six ends close and repeated/nil
// cleanup is harmless; no process or protocol lifecycle is substituted by this check.
// TestCommandPipesCloseReleasesCompleteAllocation проверяет закрытие всех шести концов и безопасность
// повторной или nil-очистки; жизненный цикл процесса или протокола этим не подменяется.
func TestCommandPipesCloseReleasesCompleteAllocation(t *testing.T) {
	pipes, err := NewCommandPipes(&exec.Cmd{}, "synthetic app-server")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipes.Close)
	pipes.Close()
	assertCommandPipeFilesClosed(t, []*os.File{pipes.Stdin, pipes.Stdout, pipes.Stderr, pipes.StdinChild, pipes.StdoutChild, pipes.StderrChild})
	pipes.Close()
	(*CommandPipes)(nil).Close()
}

// assertCommandPipeFilesClosed requires closed handles explicitly, not another Stat failure
// mistaken for cleanup success.
// assertCommandPipeFilesClosed требует именно закрытые handles, а не другую ошибку Stat,
// принятую за успешную очистку.
func assertCommandPipeFilesClosed(t *testing.T, files []*os.File) {
	t.Helper()
	for _, file := range files {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("command pipe descriptor remained open")
		}
	}
}
