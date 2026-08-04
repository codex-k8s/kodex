// Package handoff публикует один signed terminal envelope в controller-owned ConfigMap.
package handoff

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const dataKey = "handoff.json"

func Publish(ctx context.Context, input model.Input, value runtimecontract.HandoffV2) error {
	key, err := readPrivateKey(input.CredentialFiles.HandoffPrivateKey)
	if err != nil {
		return err
	}
	raw, err := runtimecontract.SignHandoffV2(value, input.CredentialFiles.HandoffKeyID, key)
	if err != nil {
		return err
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load Kubernetes workload identity")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create Kubernetes handoff client")
	}
	configMaps := client.CoreV1().ConfigMaps(input.PodNamespace)
	current, err := configMaps.Get(ctx, input.HandoffConfigMap, metav1.GetOptions{})
	if err != nil || current.Annotations["runtime.mattercodex.dev/execution-id"] != input.ExecutionID ||
		current.Annotations["runtime.mattercodex.dev/turn-id"] != input.TurnID ||
		current.Annotations["runtime.mattercodex.dev/grant-generation"] != fmt.Sprint(input.GrantGeneration) {
		return errors.New("controller-owned handoff target is invalid")
	}
	if existing := current.BinaryData[dataKey]; len(existing) != 0 {
		if bytes.Equal(existing, raw) {
			return nil
		}
		return errors.New("terminal handoff already has a different winner")
	}
	if current.BinaryData == nil {
		current.BinaryData = map[string][]byte{}
	}
	current.BinaryData[dataKey] = raw
	if _, err := configMaps.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return errors.New("publish terminal handoff")
	}
	return nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return nil, errors.New("read handoff private key")
	}
	if len(raw) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(bytes.Clone(raw)), nil
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || block.Type != "PRIVATE KEY" {
		return nil, errors.New("decode handoff private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	key, ok := parsed.(ed25519.PrivateKey)
	if err != nil || !ok || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("parse handoff private key")
	}
	return bytes.Clone(key), nil
}
