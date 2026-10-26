# Чистые predicates общей runtime render/readback границы.
# Input принадлежит caller: первые два predicates получают массив ресурсов,
# третий — выбранный NetworkPolicy. Порядок прежних clauses сохранён;
# module не читает env, не меняет JSON, не обращается к API и не выдаёт полномочий.
def kodex_runtime_namespace_access:
  any(.[]; .kind == "Namespace" and .metadata.name == "kodex-runtime") and
  any(.[];
    .kind == "ServiceAccount" and .metadata.name == "agent-runner" and
    .metadata.namespace == "kodex-runtime") and
  any(.[];
    .kind == "RoleBinding" and .metadata.name == "runtime-controller-workloads" and
    .metadata.namespace == "kodex-runtime" and
    .subjects == [{"kind":"ServiceAccount","name":"runtime-controller","namespace":"kodex-system"}]) and
  any(.[];
    .kind == "RoleBinding" and .metadata.name == "secret-broker-runtime-secrets" and
    .metadata.namespace == "kodex-runtime" and
    .subjects == [{"kind":"ServiceAccount","name":"secret-broker","namespace":"kodex-system"}]) and
  any(.[];
    .kind == "Role" and .metadata.name == "secret-broker-runtime-secrets" and
    .metadata.namespace == "kodex-runtime" and .rules == [{
      "apiGroups":[""],
      "resources":["secrets"],
      "verbs":["get","list","create","update","delete"]
    }]);

def kodex_controller_role_no_system_secrets:
  any(.[];
    .kind == "Role" and .metadata.name == "runtime-controller" and
    .metadata.namespace == "kodex-system" and .rules == [{
      "apiGroups":["coordination.k8s.io"],
      "resources":["leases"],
      "verbs":["get","create","update","patch"]
    }]) and
  ([ .[] |
    select(.kind == "Role" and .metadata.namespace == "kodex-system" and
      (.metadata.name == "runtime-controller" or .metadata.name == "secret-broker")) |
    .rules[]? | .resources[]? ] | index("secrets") == null);

def kodex_session_archive_storage_policy:
    .spec.podSelector.matchLabels["session-archive.kodex.dev/managed"] == "true" and
    (.spec.policyTypes | sort) == (["Egress","Ingress"] | sort) and
    ((.spec.ingress // []) | length) == 0 and (.spec.egress | length) == 2 and
    ([.spec.egress[] | select(
      (.to | length) == 1 and
      .to[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kube-system" and
      .to[0].podSelector.matchLabels["k8s-app"] == "kube-dns" and
      ([.ports[] | [.protocol,.port]] | sort) == ([ ["TCP",53], ["UDP",53] ] | sort)
    )] | length) == 1 and
    ([.spec.egress[] | select(
      (.to | length) == 1 and
      .to[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kodex-system" and
      .to[0].podSelector.matchLabels["app.kubernetes.io/name"] == "seaweedfs" and
      .to[0].podSelector.matchLabels["app.kubernetes.io/component"] == "object-storage" and
      (.ports | length) == 1 and .ports[0].protocol == "TCP" and .ports[0].port == 8333
    )] | length) == 1;
