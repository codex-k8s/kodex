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

const rejected = "authority executable proof rejected"

type proof struct {
	Version      int    `json:"version"`
	Role         string `json:"role"`
	PID          int    `json:"pid"`
	StartTicks   string `json:"startTicks"`
	BinarySHA256 string `json:"binarySHA256"`
}

// Утилита читает только proc exe/stat текущего PID namespace, без env/credentials.
// Она не выдаёт authority и не обращается к рабочим RPC или PostgreSQL.
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, rejected)
		os.Exit(1)
	}
}

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
