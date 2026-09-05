import type {
  ManagedConfiguration,
  ManagedConfigurationConsumer,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";

export function canValidate(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
): boolean {
  return (
    configuration.managedBy === "UI" &&
    ["DRAFT", "INVALID"].includes(revision.state)
  );
}
export function canChangeDraft(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
): boolean {
  return (
    configuration.managedBy === "UI" &&
    ["DRAFT", "VALID", "INVALID"].includes(revision.state)
  );
}
export function canPublish(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
): boolean {
  return configuration.managedBy === "UI" && revision.state === "VALID";
}
export function consumerKey(consumer: ManagedConfigurationConsumer): string {
  return JSON.stringify([consumer.kind, consumer.ref]);
}
export function selectedConsumers(
  consumers: readonly ManagedConfigurationConsumer[],
  keys: readonly string[],
): ManagedConfigurationConsumer[] {
  const wanted = new Set(keys);
  const result = consumers.filter((consumer) =>
    wanted.has(consumerKey(consumer)),
  );
  if (
    result.length !== wanted.size ||
    new Set(result.map(consumerKey)).size !== result.length
  )
    throw new Error("Invalid configuration consumer selection");
  return result;
}
