package pitr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestExecutorСоздаётОдинCNPGClusterИПубликуетПроверяемыйEvidence(
	t *testing.T,
) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	key, err := internalrpcauth.GenerateES256Key("restore-evidence-g1")
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("bound-kubernetes-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RestoreState{
		Version: model.ContractVersion, RestoreID: "55555555-5555-4555-8555-555555555555",
		DatabaseClusterID:    "internal-rpc-authority-primary",
		BackupManifestDigest: strings.Repeat("a", 64),
		RecoveryTargetUnix:   now.Add(-time.Hour).Unix(), Phase: "PREPARED",
		RestoreEpoch: 3, CoordinationRevision: 8, ControllerGeneration: 4,
		WorkloadSetRevision: 6, AnchorRevision: 9,
		EvidenceDigest: strings.Repeat("b", 64),
		ExpectedTargets: map[string]model.RestoreExpectedTarget{
			"control-plane.authorization-verifier": {
				TargetID: "control-plane.authorization-verifier",
			},
		},
		ACKs: map[string]model.RestoreACKRecord{
			"control-plane.authorization-verifier": {
				TargetID: "control-plane.authorization-verifier",
			},
		},
	}
	var mutex sync.Mutex
	var desired desiredCluster
	var evidence secretEnvelope
	clusterCreated := false
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			mutex.Lock()
			defer mutex.Unlock()
			if request.Header.Get("Authorization") !=
				"Bearer bound-kubernetes-token" {
				http.Error(response, "forbidden", http.StatusForbidden)
				return
			}
			switch {
			case request.Method == http.MethodGet &&
				strings.Contains(request.URL.Path, "/clusters/"):
				if !clusterCreated {
					http.Error(response, "missing", http.StatusNotFound)
					return
				}
				rawSpec, _ := json.Marshal(desired.Spec)
				_ = json.NewEncoder(response).Encode(clusterEnvelope{
					APIVersion: cnpgAPIVersion,
					Kind:       "Cluster",
					Metadata: objectMetadata{
						Name: desired.Metadata.Name, Namespace: "mattercodex-system",
						UID: "restored-cluster-uid", ResourceVersion: "42",
						Generation: 2, Annotations: desired.Metadata.Annotations,
					},
					Spec: rawSpec,
					Status: clusterStatus{
						ObservedGeneration: 2, Phase: "Cluster in healthy state",
						CurrentPrimary: desired.Metadata.Name + "-1",
						ReadyInstances: 3, TimelineID: 7,
						InstancesReported: map[string]instanceReportedState{
							desired.Metadata.Name + "-1": {
								IsPrimary: true, TimelineID: 7,
							},
						},
						Conditions: []clusterCondition{{
							Type: "Ready", Status: "True", Reason: "ClusterIsReady",
						}},
					},
				})
			case request.Method == http.MethodPost &&
				strings.HasSuffix(request.URL.Path, "/clusters"):
				if err := json.NewDecoder(request.Body).Decode(&desired); err != nil {
					http.Error(response, "invalid", http.StatusBadRequest)
					return
				}
				clusterCreated = true
				response.WriteHeader(http.StatusCreated)
			case request.Method == http.MethodGet &&
				strings.HasSuffix(request.URL.Path, "/secrets/internal-rpc-authority-restore-evidence"):
				if evidence.APIVersion == "" {
					evidence.APIVersion = "v1"
					evidence.Kind = "Secret"
					evidence.Type = "Opaque"
					evidence.Metadata.Name = "internal-rpc-authority-restore-evidence"
					evidence.Metadata.Namespace = "mattercodex-system"
					evidence.Metadata.UID = "evidence-secret-uid"
					evidence.Metadata.ResourceVersion = "10"
					evidence.Metadata.Annotations = map[string]string{
						"mattercodex.dev/restore-anchor-revision":        "9",
						"mattercodex.dev/restore-evidence-digest-sha256": strings.Repeat("b", 64),
					}
					evidence.Data = map[string]string{evidenceDataKey: ""}
				}
				_ = json.NewEncoder(response).Encode(evidence)
			case request.Method == http.MethodPut &&
				strings.HasSuffix(request.URL.Path, "/secrets/internal-rpc-authority-restore-evidence"):
				if err := json.NewDecoder(request.Body).Decode(&evidence); err != nil {
					http.Error(response, "invalid", http.StatusBadRequest)
					return
				}
				evidence.Metadata.ResourceVersion = "11"
				_ = json.NewEncoder(response).Encode(evidence)
			default:
				http.Error(response, "unexpected", http.StatusNotFound)
			}
		},
	))
	defer server.Close()
	executor := &Executor{
		config: ExecutorConfig{
			TokenFile: tokenFile, Namespace: "mattercodex-system",
			EvidenceSecretName: "internal-rpc-authority-restore-evidence",
			BarmanObjectName:   "backup", BarmanServerName: "internal-rpc-authority-primary",
			PostgresImage: "registry/postgres@sha256:" + strings.Repeat("c", 64),
			StorageClass:  "postgresql", StorageSize: "20Gi", Instances: 3,
		},
		client: server.Client(), privateKey: key,
		clustersURL: server.URL + "/apis/postgresql.cnpg.io/v1/namespaces/mattercodex-system/clusters",
		evidenceURL: server.URL + "/api/v1/namespaces/mattercodex-system/secrets/internal-rpc-authority-restore-evidence",
		now:         func() time.Time { return now },
	}
	if err := executor.Execute(t.Context(), state); err == nil ||
		!strings.Contains(err.Error(), "creation submitted") {
		t.Fatalf("first execution did not submit one cluster: %v", err)
	}
	if err := executor.Execute(t.Context(), state); err != nil {
		t.Fatalf("second execution did not publish evidence: %v", err)
	}
	if err := executor.Execute(t.Context(), state); err != nil {
		t.Fatalf("semantic retry after response loss failed: %v", err)
	}
	verifier := &Verifier{
		config: Config{
			TokenFile: tokenFile, Namespace: "mattercodex-system",
			EvidenceSecretName: "internal-rpc-authority-restore-evidence",
		},
		client: server.Client(), evidenceURL: executor.evidenceURL,
		publicKey: key.PublicOnly(), now: func() time.Time { return now },
	}
	verified, err := verifier.VerifyCompletedEvidence(t.Context(), state)
	if err != nil {
		t.Fatalf("published evidence did not verify: %v", err)
	}
	if verified.AnchorRevision != 10 ||
		verified.RestoreEpoch != 3 ||
		verified.RestoredTimelineID != 7 ||
		verified.RestoredClusterUID != "restored-cluster-uid" {
		t.Fatalf("verified evidence binding is incomplete: %#v", verified)
	}
	mutex.Lock()
	evidence.Metadata.Annotations["mattercodex.dev/restored-timeline-id"] = "8"
	mutex.Unlock()
	if _, err := verifier.VerifyCompletedEvidence(t.Context(), state); err == nil {
		t.Fatal("served timeline annotation mutation accepted")
	}
}
