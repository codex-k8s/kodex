import type {
  IntegrationCapability,
  IntegrationConnection,
  IntegrationGrantInput,
} from "@/shared/api/generated/openapi/types.gen";

type KubernetesGrantFields = Pick<
  IntegrationGrantInput,
  "kubernetesNamespace" | "kubernetesGrantRef"
>;

export function validKubernetesGrantNamespace(
  operation: string,
  namespace: string,
): boolean {
  if (
    operation === "kubernetes.list.all-namespaces" ||
    operation === "kubernetes.watch.all-namespaces"
  )
    return namespace === "@all-namespaces";
  return (
    namespace === "@cluster" ||
    (namespace.length <= 63 &&
      /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/.test(namespace))
  );
}

// Ref выводится из exact owner snapshot выбранной строки, а не из текста поля.
// Несколько совпавших rows закрывают форму; server повторяет ownership/OCC checks.
export function kubernetesGrantScopeFields(
  connection: IntegrationConnection | undefined,
  capability: IntegrationCapability | undefined,
  namespace: string,
  recipientKind: "AGENT" | "WORKFLOW",
  recipientRef: string,
): KubernetesGrantFields | undefined {
  if (!connection || !capability || !recipientRef) return undefined;
  if (capability.resourceKind !== "KUBERNETES_TARGET")
    return namespace === "" ? {} : undefined;
  const target = connection.publicConfiguration.target_ref;
  if (
    connection.definitionKey !== "kubernetes-managed" ||
    connection.credentialDelivery?.kind !== "BROKER_IDENTITY" ||
    connection.credentialDelivery.brokerPurpose !== "KUBERNETES_TARGET" ||
    typeof target !== "string" ||
    !target
  )
    return undefined;
  if (!validKubernetesGrantNamespace(capability.operation, namespace))
    return undefined;
  const matches = connection.grants.filter(
    (grant) =>
      grant.capabilityKey === capability.key &&
      grant.resourceScope.kind === "KUBERNETES_TARGET" &&
      grant.resourceScope.values.target_ref === target &&
      grant.resourceScope.values.namespace === namespace &&
      (recipientKind === "AGENT"
        ? grant.agentRef === recipientRef && !grant.workflowRef
        : grant.workflowRef === recipientRef && !grant.agentRef),
  );
  if (matches.length > 1) return undefined;
  return {
    kubernetesNamespace: namespace,
    ...(matches[0] ? { kubernetesGrantRef: matches[0].ref } : {}),
  };
}
