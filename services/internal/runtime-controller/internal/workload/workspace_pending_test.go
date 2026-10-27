package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TestWorkspacePendingPaginationReachesTailAndRestartsExpiredCursor exercises the actual Kubernetes REST reader.
// A mixed collection exceeds 128 tickets; malformed V2 pages cannot starve a valid last-page observation.
// An expired continuation restarts scheduling without granting writer or deletion authority.
// TestWorkspacePendingPaginationReachesTailAndRestartsExpiredCursor проверяет actual Kubernetes REST reader.
// Смешанная коллекция превышает 128 tickets; некорректные V2 pages не блокируют valid last-page observation.
// Истечение continuation перезапускает обход без выдачи writer или deletion authority.
func TestWorkspacePendingPaginationReachesTailAndRestartsExpiredCursor(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expire), func(t *testing.T) {
			manager, fakeClient, input, _ := workspaceStoragePodFixture(t)
			ctx := context.Background()
			if err := manager.ensureTicket(ctx, ticketName(input.LeaseRef), runtimecontract.RuntimeTurnPodName(input.LeaseRef), "turn", input, strings.Repeat("a", 64), nil, nil); err != nil {
				t.Fatal(err)
			}
			tail, err := fakeClient.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, ticketName(input.LeaseRef), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var tickets []corev1.Secret
			for index := 0; index < 129; index++ {
				legacy := tail.DeepCopy()
				legacy.Name = fmt.Sprintf("legacy-%03d", index)
				delete(legacy.Labels, workspaceStorageProfileAnnotation)
				delete(legacy.Annotations, workspaceStorageProfileAnnotation)
				tickets = append(tickets, *legacy)
			}
			for index := 0; index < 96; index++ {
				invalid := tail.DeepCopy()
				invalid.Name = fmt.Sprintf("invalid-%03d", index)
				invalid.Data[inputKey] = []byte(`{"invalid":true}`)
				tickets = append(tickets, *invalid)
			}
			tickets = append(tickets, *tail)
			var mu sync.Mutex
			var cursors []string
			var podReads int
			expired := false
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				writer.Header().Set("Content-Type", "application/json")
				if request.Method != http.MethodGet {
					t.Error("workspace observation attempted a mutation")
					writer.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if request.URL.Path == "/api/v1/namespaces/"+manager.config.RuntimeNamespace+"/pods/"+runtimecontract.RuntimeTurnPodName(input.LeaseRef) {
					podReads++
					writer.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(writer).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Reason: metav1.StatusReasonNotFound, Code: http.StatusNotFound})
					return
				}
				if request.URL.Path != "/api/v1/namespaces/"+manager.config.RuntimeNamespace+"/secrets" {
					t.Errorf("unexpected observation path %s", request.URL.Path)
					writer.WriteHeader(http.StatusNotFound)
					return
				}
				query := request.URL.Query()
				cursor := query.Get("continue")
				cursors = append(cursors, cursor)
				if expire && !expired && cursor != "" {
					expired = true
					writer.WriteHeader(http.StatusGone)
					_ = json.NewEncoder(writer).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Reason: metav1.StatusReasonExpired, Code: http.StatusGone})
					return
				}
				selector, err := labels.Parse(query.Get("labelSelector"))
				if err != nil || query.Get("limit") != "32" {
					t.Error("invalid bounded list request")
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				var eligible []corev1.Secret
				for _, ticket := range tickets {
					if selector.Matches(labels.Set(ticket.Labels)) {
						eligible = append(eligible, ticket)
					}
				}
				start := 0
				if cursor != "" {
					start, err = strconv.Atoi(strings.TrimPrefix(cursor, "page-"))
				}
				if err != nil || start < 0 || start >= len(eligible) {
					t.Error("invalid continuation cursor")
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				end := min(start+32, len(eligible))
				page := corev1.SecretList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "SecretList"}, Items: eligible[start:end]}
				if end < len(eligible) {
					page.Continue = fmt.Sprintf("page-%d", end)
				}
				_ = json.NewEncoder(writer).Encode(page)
			}))
			t.Cleanup(server.Close)
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, Timeout: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			manager.client = client
			found, failures := 0, 0
			polls := 4
			if expire {
				polls = 6
			}
			for index := 0; index < polls; index++ {
				observations, err := manager.PendingWorkspaceQuiescence(ctx)
				if err != nil {
					failures++
					continue
				}
				for _, observation := range observations {
					if observation.Input.LeaseRef != input.LeaseRef || observation.BlockReason != "POD_MISSING" || observation.Record.Joined {
						t.Fatal("missing Pod became quiescent or foreign input was observed")
					}
					found++
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if found != 1 || podReads != 1 || failures != map[bool]int{false: 0, true: 1}[expire] || manager.workspaceObservationContinue != "" {
				t.Fatalf("tail did not complete bounded cycle: found=%d reads=%d failures=%d cursors=%v", found, podReads, failures, cursors)
			}
			if expire && (len(cursors) < 3 || cursors[2] != "") {
				t.Fatal("expired continuation was not restarted")
			}
		})
	}
}
