// Package main exposes a bounded, local-only containerd image inventory and digest-preconditioned deletion.
// Пакет main предоставляет ограниченный локальный inventory containerd и удаление образа с проверкой digest сервером.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	containersapi "github.com/containerd/containerd/api/services/containers/v1"
	imagesapi "github.com/containerd/containerd/api/services/images/v1"
	"github.com/containerd/containerd/api/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// failureText hides runtime response bodies and limits diagnostics to one closed message.
// failureText скрывает тела ответов runtime и ограничивает диагностику одним закрытым сообщением.
const failureText = "Installation image operation rejected: scope, usage, target or readback mismatch"

// imagePrefix is the repository-owned local image namespace; other repositories are never deleted.
// imagePrefix задаёт принадлежащую репозиторию локальную область образов; другие репозитории не удаляются.
const imagePrefix = "registry.local.kodex/kodex/"

// referencePattern excludes selectors, path traversal and arbitrary registry destinations.
// referencePattern исключает selectors, обход пути и произвольные registry destinations.
var referencePattern = regexp.MustCompile(`^registry\.local\.kodex/kodex/[a-z0-9][a-z0-9-]*(?::[A-Za-z0-9][A-Za-z0-9_.-]{0,127}|@sha256:[a-f0-9]{64})$`)

// digestPattern requires a complete SHA256 content address rather than a tag or abbreviated digest.
// digestPattern требует полный SHA256 адрес содержимого, а не tag или сокращённый digest.
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// imageRecord projects only public image identity; arbitrary labels and container configuration are excluded.
// imageRecord проецирует только публичную identity образа; произвольные labels и конфигурация контейнера исключены.
type imageRecord struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// inspect reads bounded metadata through the generated API without accessing content blobs or secrets.
// inspect читает ограниченные metadata через generated API, не обращаясь к blobs или секретам.
func inspect(ctx context.Context, client imagesapi.ImagesClient) ([]imageRecord, error) {
	response, err := client.List(ctx, &imagesapi.ListImagesRequest{})
	if err != nil || response == nil || len(response.Images) > 4096 {
		return nil, errors.New(failureText)
	}
	result := make([]imageRecord, 0, len(response.Images))
	seen := make(map[string]bool)
	for _, image := range response.Images {
		if image == nil || image.Name == "" || image.Target == nil || !digestPattern.MatchString(image.Target.Digest) || seen[image.Name] {
			return nil, errors.New(failureText)
		}
		seen[image.Name] = true
		result = append(result, imageRecord{Name: image.Name, Digest: image.Target.Digest})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

// erase requires an unused exact target, preserves foreign aliases, and sends the digest as an atomic server precondition.
// erase требует неиспользуемый exact target, сохраняет чужие aliases и передаёт digest как атомарное условие серверу.
func erase(ctx context.Context, images imagesapi.ImagesClient, containers containersapi.ContainersClient, name, expected string) error {
	if !referencePattern.MatchString(name) || !digestPattern.MatchString(expected) {
		return errors.New(failureText)
	}
	if _, suffix, immutable := strings.Cut(name, "@"); immutable && suffix != expected {
		return errors.New(failureText)
	}
	records, err := inspect(ctx, images)
	if err != nil {
		return err
	}
	byName := make(map[string]string, len(records))
	for _, record := range records {
		byName[record.Name] = record.Digest
		if record.Digest == expected && !strings.HasPrefix(record.Name, imagePrefix) {
			return errors.New(failureText)
		}
	}
	if current, found := byName[name]; !found {
		return nil
	} else if current != expected {
		return errors.New(failureText)
	}
	active, err := containers.List(ctx, &containersapi.ListContainersRequest{})
	if err != nil || active == nil || len(active.Containers) > 4096 {
		return errors.New(failureText)
	}
	for _, container := range active.Containers {
		if container == nil || container.Image == "" || container.Image == name || byName[container.Image] == expected {
			return errors.New(failureText)
		}
	}
	_, err = images.Delete(ctx, &imagesapi.DeleteImageRequest{Name: name, Sync: true, Target: &types.Descriptor{Digest: expected}})
	if err != nil && status.Code(err) != codes.NotFound {
		return errors.New(failureText)
	}
	_, err = images.Get(ctx, &imagesapi.GetImageRequest{Name: name})
	if status.Code(err) != codes.NotFound {
		return errors.New(failureText)
	}
	return nil
}

// run connects exclusively to the local k3s Unix socket; no caller-selected endpoint or namespace is accepted.
// run подключается только к локальному Unix socket k3s; caller не выбирает endpoint или namespace.
func run(arguments []string) error {
	if len(arguments) != 1 && len(arguments) != 3 || len(arguments) == 1 && arguments[0] != "inspect" || len(arguments) == 3 && arguments[0] != "delete" {
		return errors.New(failureText)
	}
	connection, err := grpc.NewClient("unix:///run/k3s/containerd/containerd.sock", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return errors.New(failureText)
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "containerd-namespace", "k8s.io")
	images := imagesapi.NewImagesClient(connection)
	if arguments[0] == "inspect" {
		value, inspectErr := inspect(ctx, images)
		if inspectErr != nil {
			return inspectErr
		}
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	if err := erase(ctx, images, containersapi.NewContainersClient(connection), arguments[1], arguments[2]); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ABSENT"})
}

// main reports a closed diagnostic and nonzero status without echoing raw RPC errors or caller input.
// main возвращает закрытую диагностику и ненулевой status, не раскрывая raw RPC errors или caller input.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, failureText)
		os.Exit(1)
	}
}
