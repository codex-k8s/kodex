package serviceruntime

import (
	"fmt"
	"os"
	"os/exec"
)

// commandPipeErrorTemplate preserves a caller-owned diagnostic label and a closed stream name,
// never the underlying operating-system error, descriptor path or child payload.
// commandPipeErrorTemplate сохраняет заданную вызывающим кодом диагностическую метку и закрытое имя
// потока, но не исходную ошибку ОС, путь дескриптора или данные дочернего процесса.
const commandPipeErrorTemplate = "create %s %s stream"

// CommandPipes owns three explicit os.Pipe pairs assigned directly to an unstarted exec.Cmd.
// Stdin writes to StdinChild; StdoutChild/StderrChild write to Stdout/Stderr. The caller closes
// child ends after Start and owns parent ends through reader drain/join; Cmd.Wait does not own
// these caller-created handles. This type neither starts processes nor manages cancellation.
// CommandPipes владеет тремя явными os.Pipe, назначенными ещё не запущенной exec.Cmd.
// Stdin пишет в StdinChild, StdoutChild/StderrChild — в Stdout/Stderr. Вызывающий код закрывает
// child ends после Start и владеет parent ends до drain/join readers; Cmd.Wait не владеет
// созданными вызывающим handles. Тип не запускает процесс и не управляет отменой.
type CommandPipes struct {
	Stdin       *os.File
	Stdout      *os.File
	Stderr      *os.File
	StdinChild  *os.File
	StdoutChild *os.File
	StderrChild *os.File
}

// NewCommandPipes allocates and assigns explicit child streams without Start, goroutines, contexts
// or protocol policy. command must be nonnil and unstarted; label is trusted diagnostic text, not
// user input or a secret. On allocation failure all acquired handles are closed and no pipes escape.
// NewCommandPipes выделяет и назначает явные потоки ребёнка без Start, goroutine, context или protocol
// policy. command обязана быть ненулевой и не запущенной; label — доверенная диагностическая метка,
// не ввод пользователя и не секрет. При ошибке все полученные handles закрываются и не возвращаются.
func NewCommandPipes(command *exec.Cmd, label string) (*CommandPipes, error) {
	return newCommandPipes(command, label, os.Pipe)
}

// newCommandPipes exposes only the pipe factory to deterministic allocation-failure tests.
// Stdin is assigned after its pair exists; output assignments occur only after all pairs exist,
// matching the consumer startup ordering. Operating-system causes are deliberately not wrapped.
// newCommandPipes открывает лишь фабрику pipe для детерминированных тестов отказов выделения.
// Stdin назначается после создания пары, output — лишь после всех пар, сохраняя порядок запуска
// потребителей. Исходные ошибки ОС намеренно не включаются в возвращённую цепочку.
func newCommandPipes(command *exec.Cmd, label string, open func() (*os.File, *os.File, error)) (*CommandPipes, error) {
	var pipes CommandPipes
	var err error
	pipes.StdinChild, pipes.Stdin, err = open()
	if err != nil {
		pipes.Close()
		return nil, fmt.Errorf(commandPipeErrorTemplate, label, "request")
	}
	command.Stdin = pipes.StdinChild
	pipes.Stdout, pipes.StdoutChild, err = open()
	if err != nil {
		pipes.Close()
		return nil, fmt.Errorf(commandPipeErrorTemplate, label, "response")
	}
	pipes.Stderr, pipes.StderrChild, err = open()
	if err != nil {
		pipes.Close()
		return nil, fmt.Errorf(commandPipeErrorTemplate, label, "diagnostic")
	}
	command.Stdout, command.Stderr = pipes.StdoutChild, pipes.StderrChild
	return &pipes, nil
}

// Close best-effort closes every acquired end, including a partial allocation; nil is harmless.
// It is allocation cleanup, not process shutdown or proof that readers joined. Successful callers
// normally close individual ends at their existing lifecycle points instead of closing all at once.
// Close пытается закрыть все полученные концы, включая неполное выделение; nil безопасен.
// Это очистка дескрипторов, не остановка процесса и не доказательство join readers. После успеха
// вызывающий код обычно закрывает отдельные концы в своих прежних точках жизненного цикла.
func (pipes *CommandPipes) Close() {
	if pipes == nil {
		return
	}
	for _, file := range []*os.File{pipes.Stdin, pipes.Stdout, pipes.StdoutChild, pipes.Stderr, pipes.StderrChild, pipes.StdinChild} {
		if file != nil {
			_ = file.Close()
		}
	}
}
