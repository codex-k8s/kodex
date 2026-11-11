package main

import (
	"context"
	"strings"
	"testing"

	containersapi "github.com/containerd/containerd/api/services/containers/v1"
	imagesapi "github.com/containerd/containerd/api/services/images/v1"
	"github.com/containerd/containerd/api/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fixtureImages simulates only image metadata and records the atomic server precondition.
// fixtureImages моделирует только metadata образов и сохраняет атомарное условие запроса серверу.
type fixtureImages struct {
	imagesapi.ImagesClient
	records []*imagesapi.Image
	deleted *imagesapi.DeleteImageRequest
	lostACK bool
}

// List returns synthetic records without any filesystem or daemon access.
// List возвращает synthetic записи без доступа к filesystem или daemon.
func (client *fixtureImages) List(context.Context, *imagesapi.ListImagesRequest, ...grpc.CallOption) (*imagesapi.ListImagesResponse, error) {
	return &imagesapi.ListImagesResponse{Images: client.records}, nil
}

// Delete records the target and models a committed deletion whose acknowledgement may be lost.
// Delete фиксирует target и моделирует завершённое удаление с возможной потерей подтверждения.
func (client *fixtureImages) Delete(_ context.Context, request *imagesapi.DeleteImageRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	client.deleted = request
	client.records = nil
	if client.lostACK {
		return nil, status.Error(codes.Unavailable, "synthetic lost acknowledgement")
	}
	return &emptypb.Empty{}, nil
}

// Get models authoritative absence after deletion without inventing a replacement.
// Get моделирует авторитетное отсутствие после удаления без создания replacement.
func (client *fixtureImages) Get(context.Context, *imagesapi.GetImageRequest, ...grpc.CallOption) (*imagesapi.GetImageResponse, error) {
	return nil, status.Error(codes.NotFound, "synthetic absence")
}

// fixtureContainers provides synthetic container image bindings for foreign-consumer checks.
// fixtureContainers предоставляет synthetic привязки образов контейнеров для проверки чужих consumers.
type fixtureContainers struct {
	containersapi.ContainersClient
	bindings []*containersapi.Container
}

// List returns the exact configured bindings and never contacts a runtime.
// List возвращает точные настроенные привязки и не обращается к runtime.
func (client *fixtureContainers) List(context.Context, *containersapi.ListContainersRequest, ...grpc.CallOption) (*containersapi.ListContainersResponse, error) {
	return &containersapi.ListContainersResponse{Containers: client.bindings}, nil
}

// TestErasePreconditionsAndRejoin proves server-side target binding and absence-only rejoin after lost ACK.
// TestErasePreconditionsAndRejoin доказывает привязку target на сервере и rejoin только через отсутствие после lost ACK.
func TestErasePreconditionsAndRejoin(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	name := imagePrefix + "control-plane@" + digest
	for _, lost := range []bool{false, true} {
		images := &fixtureImages{records: []*imagesapi.Image{{Name: name, Target: &types.Descriptor{Digest: digest}}}, lostACK: lost}
		err := erase(context.Background(), images, &fixtureContainers{}, name, digest)
		if (err != nil) != lost {
			t.Fatalf("unexpected first-call result: lost=%v error=%v", lost, err)
		}
		if images.deleted == nil || !images.deleted.Sync || images.deleted.Target.GetDigest() != digest || images.deleted.Name != name {
			t.Fatal("atomic target precondition missing")
		}
		previous := images.deleted
		if err := erase(context.Background(), images, &fixtureContainers{}, name, digest); err != nil || images.deleted != previous {
			t.Fatal("rejoin repeated deletion instead of accepting authoritative absence")
		}
	}
}

// TestEraseRejectsForeignAliasesConsumersAndTargetDrift checks failures before any mutation request.
// TestEraseRejectsForeignAliasesConsumersAndTargetDrift проверяет отказы до любого mutation запроса.
func TestEraseRejectsForeignAliasesConsumersAndTargetDrift(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	name := imagePrefix + "control-plane:fixture"
	for _, scenario := range []string{"foreign-alias", "consumer", "replacement", "outside-scope", "immutable-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			images := &fixtureImages{records: []*imagesapi.Image{{Name: name, Target: &types.Descriptor{Digest: digest}}}}
			containers := &fixtureContainers{}
			reference := name
			switch scenario {
			case "foreign-alias":
				images.records = append(images.records, &imagesapi.Image{Name: "example.invalid/foreign:fixture", Target: &types.Descriptor{Digest: digest}})
			case "consumer":
				containers.bindings = []*containersapi.Container{{Image: name}}
			case "replacement":
				images.records[0].Target.Digest = "sha256:" + strings.Repeat("b", 64)
			case "outside-scope":
				reference = "example.invalid/foreign:fixture"
			case "immutable-mismatch":
				reference = imagePrefix + "control-plane@sha256:" + strings.Repeat("b", 64)
			}
			if err := erase(context.Background(), images, containers, reference, digest); err == nil || images.deleted != nil {
				t.Fatal("unsafe image deletion accepted")
			}
		})
	}
}
