package kubernetestarget

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// authorizeMetadata repeats selected identity and the exact non-resource permission; local help/config and whoami do not borrow a resource grant.
// authorizeMetadata повторно проверяет selected identity и exact non-resource permission; локальные help/config и whoami не заимствуют resource grant.
func (prepared *PreparedResourceOperation) authorizeMetadata(ctx context.Context) error {
	if !integrationpackage.KubernetesMetadataRead(prepared.operation.Key) || !managedkubernetes.ValidDigest(prepared.subjectDigest) {
		return errors.New(resourceOperationInvalid)
	}
	if prepared.operation.Key == "kubernetes.version" {
		return prepared.credential.Authorize(ctx, prepared.subjectDigest, []AccessRequirement{{Discovery: managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryVersion}, Verb: "get"}})
	}
	digest, err := prepared.credential.ObserveIdentity(ctx)
	if err != nil || digest != prepared.subjectDigest {
		return errors.New(identityChanged)
	}
	return nil
}

// executeMetadataRequest returns a bounded typed projection under the existing single-attempt cancel/join lifetime; it never follows response URLs or loads resource objects.
// executeMetadataRequest возвращает bounded typed projection внутри existing single-attempt cancel/join lifetime; не переходит по response URLs и не загружает resource objects.
func (prepared *PreparedResourceOperation) executeMetadataRequest(ctx context.Context) (value.ResourceOperationResult, error) {
	failed := value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonTargetUnavailable}
	if prepared.credential.VerifyClusterIdentity(ctx) != nil || prepared.authorizeMetadata(ctx) != nil {
		return failed, errors.New(resourceOperationInvalid)
	}
	var result map[string]any
	switch prepared.operation.Key {
	case "kubernetes.config.metadata":
		result = map[string]any{"targetRef": prepared.credential.target.ID, "targetRevision": prepared.credential.target.Revision, "clusterUID": prepared.credential.target.ClusterUID, "managed": true}
	case "kubernetes.help":
		var err error
		result, err = prepared.metadataHelp()
		if err != nil {
			return failed, err
		}
	case "kubernetes.version", "kubernetes.identity":
		client, closeIdle, err := prepared.credential.NewClient(30 * time.Second)
		if err != nil {
			return failed, errors.New(resourceOperationInvalid)
		}
		defer closeIdle()
		failed.Attempted = true
		if prepared.operation.Key == "kubernetes.identity" {
			subject, err := prepared.credential.observeSubjectInfo(ctx, client)
			digest, digestErr := SubjectDigest(subject)
			if err != nil || digestErr != nil || digest != prepared.subjectDigest {
				return failed, errors.New(identityChanged)
			}
			// Authentication extras may contain private identity-provider data and are deliberately not projected.
			// Authentication extras могут содержать приватные данные identity provider и намеренно не проецируются.
			result = map[string]any{"username": subject.Username, "uid": subject.UID, "groups": subject.Groups}
		} else {
			request, err := prepared.credential.DiscoveryRequest(ctx, managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryVersion})
			if err != nil {
				return failed, errors.New(resourceOperationInvalid)
			}
			raw, status, err := readResourceHTTPDocument(client, request, maximumReviewBytes)
			defer clear(raw)
			if err != nil || status != http.StatusOK {
				return failed, errors.New(resourceOperationInvalid)
			}
			object, err := decodeResourceJSON(raw, maximumReviewBytes, 8)
			if err != nil {
				return failed, errors.New(resourceOperationInvalid)
			}
			server := map[string]any{}
			for _, field := range []string{"major", "minor", "gitVersion", "gitCommit", "gitTreeState", "buildDate", "goVersion", "compiler", "platform"} {
				v, exists := object[field]
				if !exists {
					continue
				}
				text, valid := v.(string)
				if !valid || len(text) > 256 || strings.ContainsAny(text, "\x00\r\n") {
					return failed, errors.New(resourceOperationInvalid)
				}
				server[field] = text
			}
			if server["gitVersion"] == nil || server["gitVersion"] == "" || server["major"] == nil || server["minor"] == nil {
				return failed, errors.New(resourceOperationInvalid)
			}
			result = map[string]any{"serverVersion": server}
		}
	default:
		return failed, errors.New(resourceOperationInvalid)
	}
	if ctx.Err() != nil {
		return failed, errors.New(resourceOperationInvalid)
	}
	raw, err := marshalResourceSafeResult(result)
	if err != nil {
		return failed, err
	}
	return value.ResourceOperationResult{Outcome: "SUCCEEDED", SafeJSON: raw, Attempted: failed.Attempted}, nil
}

// metadataHelp describes the registered input language, not effective credentials, grants or installed handler readiness.
// metadataHelp описывает registered input language, не effective credentials, grants либо готовность установленного handler.
func (prepared *PreparedResourceOperation) metadataHelp() (map[string]any, error) {
	options, err := resourceOperationOptions(prepared.input)
	if err != nil {
		return nil, err
	}
	name, _ := options["commandName"].(string)
	commands := []any{}
	for _, operation := range integrationpackage.KubernetesOperations() {
		if name != "" && name != operation.Key && name != operation.Family {
			continue
		}
		entry := map[string]any{"operation": operation.Key, "family": operation.Family, "risk": operation.Risk, "approvalPolicy": operation.Approval}
		if name != "" {
			schema, err := managedkubernetes.OperationInputSchema(operation.Key)
			if err != nil {
				return nil, errors.New(resourceOperationInvalid)
			}
			entry["inputSchema"] = json.RawMessage(schema)
		}
		commands = append(commands, entry)
	}
	if len(commands) == 0 {
		return nil, errors.New(resourceOperationInvalid)
	}
	return map[string]any{"registeredCommands": commands, "effectiveReadinessChecked": false}, nil
}
