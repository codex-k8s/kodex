package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// rejected is the fixed public failure text for executable-proof refusal; the process entry point prints it instead of reflecting private filesystem or parsing errors.
// rejected — фиксированный публичный текст отказа проверки исполняемого файла; точка входа выводит его вместо отражения закрытых файловых ошибок или ошибок разбора.
const rejected = "authority executable proof rejected"

// proof is the version-one JSON observation of role, PID, process start ticks and executable SHA-256. It is unsigned local evidence, not an issued authorization grant, provider credential or trusted snapshot by itself.
// proof — JSON-наблюдение первой версии с ролью, PID, тактами запуска процесса и SHA-256 исполняемого файла. Это неподписанные локальные данные, а не выданное полномочие, реквизит провайдера или самостоятельный доверенный снимок.
type proof struct {
	Version      int    `json:"version"`
	Role         string `json:"role"`
	PID          int    `json:"pid"`
	StartTicks   string `json:"startTicks"`
	BinarySHA256 string `json:"binarySHA256"`
}

// main runs the closed executable-proof command and emits only the fixed rejection text on failure before exiting nonzero. Successful JSON goes to stdout; environment, credentials, RPC and PostgreSQL are not read by this utility.
// main запускает закрытую команду проверки исполняемого файла и при ошибке выводит только фиксированный текст отказа перед ненулевым выходом. Успешный JSON идёт в stdout; утилита не читает окружение и реквизиты и не обращается к RPC или PostgreSQL.
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, rejected)
		os.Exit(1)
	}
}

// run accepts exactly --role issuer or --role verifier, inspects the corresponding fixed binary path under the current /proc namespace, assigns the requested role to the observation and JSON-encodes it. Arbitrary executable or proc paths cannot be supplied through these arguments.
// run принимает ровно --role issuer либо --role verifier, проверяет соответствующий фиксированный путь бинаря в текущем пространстве /proc, назначает наблюдению запрошенную роль и кодирует его в JSON. Произвольный путь бинаря или proc через эти аргументы передать нельзя.
func run(args []string, out io.Writer) error {
	if len(args) != 2 || args[0] != "--role" || (args[1] != "issuer" && args[1] != "verifier") {
		return errors.New(rejected)
	}
	result, err := inspect("/proc", "/usr/local/bin/internal-rpc-authority-"+args[1])
	if err != nil {
		return err
	}
	result.Role = args[1]
	return json.NewEncoder(out).Encode(result)
}

// processTicks reads a process stat file, locates the last closing command-name delimiter, and requires the twentieth remaining field to parse as an unsigned sixty-four-bit start tick count. It returns the original field text, not a timestamp or normalized identity, and rejects incomplete or malformed records.
// processTicks читает stat процесса, находит последний закрывающий разделитель имени команды и требует разбираемое беззнаковое шестидесятичетырёхбитное число тактов запуска в двадцатом оставшемся поле. Возвращается исходный текст поля, а не временная метка или нормализованная идентичность; неполные и некорректные записи отклоняются.
func processTicks(path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(path, "stat"))
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return "", errors.New(rejected)
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 {
		return "", errors.New(rejected)
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", errors.New(rejected)
	}
	return fields[19], nil
}

// matching lists numeric positive PID entries whose exe symlink target exactly equals the expected path. Missing links are skipped for disappearing processes, other readlink failures stop enumeration, and returned names retain their original spelling; this function does not inspect executable bytes or process authority.
// matching перечисляет положительные числовые PID, у которых цель ссылки exe точно равна ожидаемому пути. Отсутствующие ссылки исчезающих процессов пропускаются, другие ошибки readlink прерывают обход, имена возвращаются в исходном виде; байты бинаря и полномочия процесса здесь не проверяются.
func matching(root, expected string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 {
			continue
		}
		path := filepath.Join(root, entry.Name())
		target, err := os.Readlink(filepath.Join(path, "exe"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if target == expected {
			found = append(found, entry.Name())
		}
	}
	return found, nil
}

// inspect requires exactly one matching process, reads start ticks, opens its nonempty regular executable up to one hundred twenty-eight MiB and hashes at most that limit plus one byte, requiring the observed size to match. It then rechecks ticks, underlying file identity and the unique PID match before returning evidence; these sequential checks do not freeze a process or sign the result.
// inspect требует ровно один совпадающий процесс, читает такты запуска, открывает его непустой обычный исполняемый файл до ста двадцати восьми МиБ и хеширует не более этого предела плюс один байт с требованием точного наблюдённого размера. Затем повторно проверяет такты, идентичность файла и единственность PID перед возвратом данных; последовательные проверки не замораживают процесс и не подписывают результат.
func inspect(root, expected string) (proof, error) {
	found, err := matching(root, expected)
	if err != nil || len(found) != 1 {
		return proof{}, errors.New(rejected)
	}
	path := filepath.Join(root, found[0])
	before, err := processTicks(path)
	if err != nil {
		return proof{}, err
	}
	file, err := os.Open(filepath.Join(path, "exe"))
	if err != nil {
		return proof{}, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 128<<20 {
		return proof{}, errors.New(rejected)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, (128<<20)+1))
	if err != nil || n != stat.Size() {
		return proof{}, errors.New(rejected)
	}
	after, err := processTicks(path)
	if err != nil || before != after {
		return proof{}, errors.New(rejected)
	}
	current, err := os.Stat(filepath.Join(path, "exe"))
	if err != nil || !os.SameFile(stat, current) {
		return proof{}, errors.New(rejected)
	}
	again, err := matching(root, expected)
	if err != nil || len(again) != 1 || again[0] != found[0] {
		return proof{}, errors.New(rejected)
	}
	pid, _ := strconv.Atoi(found[0])
	return proof{Version: 1, PID: pid, StartTicks: before, BinarySHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
