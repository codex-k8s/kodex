import type { RuntimeEnvironmentPolicy } from "../shared/api/generated/openapi/types.gen";

// Создаёт независимую синтетическую policy с прежним закрытым egress. Сохраняет переданные
// ресурсы и digest; вложенные массивы и объекты policy создаёт заново при каждом вызове.
// Production defaults и права реального окружения эта оснастка не выбирает.
export function runtimePolicyFixture(
  resources: RuntimeEnvironmentPolicy["resources"],
  digests: Pick<
    RuntimeEnvironmentPolicy,
    "resourcesDigest" | "volumesDigest" | "networkDigest" | "rbacDigest"
  >,
): RuntimeEnvironmentPolicy {
  return {
    resources: resources,
    volumes: [],
    network: {
      denyByDefault: true,
      egress: [
        { destination: "DNS", protocol: "TCP", port: 53 },
        { destination: "DNS", protocol: "UDP", port: 53 },
        { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8084 },
        { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
      ],
      webAccess: { mode: "NONE", rules: [] },
    },
    kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
    resourcesDigest: digests.resourcesDigest,
    volumesDigest: digests.volumesDigest,
    networkDigest: digests.networkDigest,
    rbacDigest: digests.rbacDigest,
  };
}
