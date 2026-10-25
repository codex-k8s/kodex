package controlplaneapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestRepositoryBundleRPCWireContract pins the pre-fix d580 field numbers, kinds, JSON names, cardinality and presence for every method while requiring method-owned message names and unchanged gRPC paths. Message identifiers may change; wire payloads and authority methods may not.
// TestRepositoryBundleRPCWireContract закрепляет номера, типы, JSON names, cardinality и presence всех полей до исправления d580 при отдельных message names каждого метода и неизменных gRPC paths. Имена типов меняются, wire payload и authority methods — нет.
func TestRepositoryBundleRPCWireContract(t *testing.T) {
	service := cp.File_controlplane_v1_repository_bundle_proto.Services().ByName("RepositoryBundlePublicationService")
	if service == nil || service.FullName() != "controlplane.v1.RepositoryBundlePublicationService" || service.Methods().Len() != 8 {
		t.Fatal("repository bundle RPC registry changed")
	}
	methods := map[string]string{
		"PrepareRepositoryBundlePublication":  cp.RepositoryBundlePublicationService_PrepareRepositoryBundlePublication_FullMethodName,
		"GetRepositoryBundlePublication":      cp.RepositoryBundlePublicationService_GetRepositoryBundlePublication_FullMethodName,
		"ClaimRepositoryBundlePublications":   cp.RepositoryBundlePublicationService_ClaimRepositoryBundlePublications_FullMethodName,
		"RenewRepositoryBundlePublication":    cp.RepositoryBundlePublicationService_RenewRepositoryBundlePublication_FullMethodName,
		"BeginRepositoryBundlePublication":    cp.RepositoryBundlePublicationService_BeginRepositoryBundlePublication_FullMethodName,
		"CompleteRepositoryBundlePublication": cp.RepositoryBundlePublicationService_CompleteRepositoryBundlePublication_FullMethodName,
		"FailRepositoryBundlePublication":     cp.RepositoryBundlePublicationService_FailRepositoryBundlePublication_FullMethodName,
		"ReadRepositoryBundlePublication":     cp.RepositoryBundlePublicationService_ReadRepositoryBundlePublication_FullMethodName,
	}
	for _, fixture := range []struct{ name, request, response string }{
		{"PrepareRepositoryBundlePublication", "lease_ref=1:leaseRef:string:false:false|fence=2:fence:string:false:false|generation=3:generation:int64:false:false|grant_ref=4:grantRef:string:false:false|archive=5:archive:controlplane.v1.RepositoryBundleArtifactPin:false:true|manifest=6:manifest:controlplane.v1.RepositoryBundleArtifactPin:false:true|base_commit_sha=7:baseCommitSha:string:false:false|idempotency_key=8:idempotencyKey:string:false:false", "proposal=1:proposal:controlplane.v1.RepositoryBundlePublication:false:true"},
		{"GetRepositoryBundlePublication", "proposal_ref=1:proposalRef:string:false:false|gate_ref=2:gateRef:string:false:false|expected_version=3:expectedVersion:int64:false:false|expected_manifest_sha256=4:expectedManifestSha256:string:false:false|offset=5:offset:int32:false:false|limit=6:limit:int32:false:false", "proposal=1:proposal:controlplane.v1.RepositoryBundlePublication:false:true|files=2:files:controlplane.v1.RepositoryBundleFile:true:false|offset=3:offset:int32:false:false|next_offset=4:nextOffset:int32:false:false|complete=5:complete:bool:false:false"},
		{"ClaimRepositoryBundlePublications", "claimant=1:claimant:string:false:false|limit=2:limit:int32:false:false", "work=1:work:controlplane.v1.RepositoryBundlePublicationWork:true:false"},
		{"RenewRepositoryBundlePublication", "lease=1:lease:controlplane.v1.RepositoryBundleLease:false:true", "lease=1:lease:controlplane.v1.RepositoryBundleLease:false:true"},
		{"BeginRepositoryBundlePublication", "lease=1:lease:controlplane.v1.RepositoryBundleLease:false:true|candidate_commit_sha=2:candidateCommitSha:string:false:false|candidate_tree_sha=3:candidateTreeSha:string:false:false|base_commit_sha=4:baseCommitSha:string:false:false|manifest_sha256=5:manifestSha256:string:false:false|archive_sha256=6:archiveSha256:string:false:false", "proposal=1:proposal:controlplane.v1.RepositoryBundlePublication:false:true|already_started=2:alreadyStarted:bool:false:false"},
		{"CompleteRepositoryBundlePublication", "lease=1:lease:controlplane.v1.RepositoryBundleLease:false:true|candidate_commit_sha=2:candidateCommitSha:string:false:false|candidate_tree_sha=3:candidateTreeSha:string:false:false|base_commit_sha=4:baseCommitSha:string:false:false|manifest_sha256=5:manifestSha256:string:false:false|archive_sha256=6:archiveSha256:string:false:false", "proposal=1:proposal:controlplane.v1.RepositoryBundlePublication:false:true"},
		{"FailRepositoryBundlePublication", "lease=1:lease:controlplane.v1.RepositoryBundleLease:false:true|failure_code=2:failureCode:string:false:false", "proposal=1:proposal:controlplane.v1.RepositoryBundlePublication:false:true"},
		{"ReadRepositoryBundlePublication", "lease=1:lease:controlplane.v1.RepositoryBundleLease:false:true", "metadata=1:metadata:controlplane.v1.RepositoryBundleArtifactPin:false:true|archive=2:archive:bytes:false:false"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			method := service.Methods().ByName(protoreflect.Name(fixture.name))
			if method == nil || method.IsStreamingClient() || method.IsStreamingServer() || methods[fixture.name] != "/controlplane.v1.RepositoryBundlePublicationService/"+fixture.name {
				t.Fatal("RPC method identity or streaming shape changed")
			}
			if string(method.Input().Name()) != fixture.name+"Request" || string(method.Output().Name()) != fixture.name+"Response" {
				t.Fatal("RPC does not own its request and response")
			}
			if repositoryBundleWireFields(method.Input()) != fixture.request || repositoryBundleWireFields(method.Output()) != fixture.response {
				t.Fatal("RPC field wire or JSON contract changed")
			}
		})
	}
	begin := &cp.BeginRepositoryBundlePublicationRequest{Lease: &cp.RepositoryBundleLease{ProposalRef: "rbp_wirefixture", Attempt: 7, Generation: 11, Claimant: "worker", Fence: "fence"}, CandidateCommitSha: strings.Repeat("a", 40), CandidateTreeSha: strings.Repeat("b", 40), BaseCommitSha: strings.Repeat("c", 40), ManifestSha256: strings.Repeat("d", 64), ArchiveSha256: strings.Repeat("e", 64)}
	wire, err := proto.Marshal(begin)
	if err != nil {
		t.Fatal("encode method-owned request")
	}
	complete := &cp.CompleteRepositoryBundlePublicationRequest{}
	if proto.Unmarshal(wire, complete) != nil {
		t.Fatal("decode preserved effect fields")
	}
	roundtrip, err := proto.Marshal(complete)
	if err != nil || !bytes.Equal(wire, roundtrip) {
		t.Fatal("Begin/Complete wire field shape diverged")
	}
	left, err := protojson.Marshal(begin)
	if err != nil {
		t.Fatal("encode Begin JSON")
	}
	right, err := protojson.Marshal(complete)
	var beginJSON, completeJSON map[string]any
	if err != nil || json.Unmarshal(left, &beginJSON) != nil || json.Unmarshal(right, &completeJSON) != nil || !reflect.DeepEqual(beginJSON, completeJSON) {
		t.Fatal("Begin/Complete JSON names diverged")
	}
}

// repositoryBundleWireFields produces a complete flat field signature including referenced message identity; absent or additional fields cannot silently pass the frozen RPC ABI fixture.
// repositoryBundleWireFields строит полную сигнатуру полей с referenced message identity; потеря либо добавление поля не проходит frozen RPC ABI fixture незаметно.
func repositoryBundleWireFields(message protoreflect.MessageDescriptor) string {
	fields := make([]string, 0, message.Fields().Len())
	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		kind := field.Kind().String()
		if field.Message() != nil {
			kind = string(field.Message().FullName())
		}
		fields = append(fields, fmt.Sprintf("%s=%d:%s:%s:%t:%t", field.Name(), field.Number(), field.JSONName(), kind, field.IsList(), field.HasPresence()))
	}
	return strings.Join(fields, "|")
}
