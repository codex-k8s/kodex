import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
  ManagedConfigurationConsumerInput,
  ManagedConfigurationImpact,
  SystemSttConfiguration,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap, asProblem } from "@/shared/api/problem";
import { requestSignal } from "@/shared/api/client";
import { mutate, etag } from "@/shared/api/mutation";
import { history, impact } from "./api";

export interface SttActivationPlan {
  configuration: ManagedConfiguration;
  revision: ManagedConfigurationRevision;
  consumer: ManagedConfigurationConsumerInput;
  digest: string;
  current?: SystemSttConfiguration;
  currentName?: string;
}
export async function readEffectiveStt(
  signal: AbortSignal,
): Promise<SystemSttConfiguration | undefined> {
  try {
    return (
      await unwrap(
        sdk.getSystemSttConfiguration({ signal: requestSignal(signal) }),
      )
    ).data;
  } catch (error) {
    const problem = asProblem(error);
    if (problem.status === 404 && problem.code === "NOT_FOUND")
      return undefined;
    throw error;
  }
}
export function checkedSttImpact(
  page: ManagedConfigurationImpact,
  configurationRef: string,
  revisionRef: string,
): ManagedConfigurationImpact {
  if (
    page.configurationRef !== configurationRef ||
    page.targetRevisionRef !== revisionRef ||
    !/^[a-f0-9]{64}$/.test(page.digest) ||
    page.nextPageToken ||
    page.total !== page.consumers.length ||
    page.total > 1 ||
    page.consumers.some(
      (c) =>
        c.kind !== "STT_SERVICE" ||
        c.ref !== "stt-tts-service" ||
        !c.revisionRef ||
        !Number.isSafeInteger(c.version) ||
        c.version < 1,
    )
  )
    throw new Error("Invalid STT binding projection");
  return page;
}
export async function readSttStatus(signal: AbortSignal) {
  const effective = await readEffectiveStt(signal);
  if (!effective) return { effective, name: undefined };
  const configuration = (await history(effective.configurationRef, signal))
    .configuration;
  if (
    configuration.ref !== effective.configurationRef ||
    configuration.kind !== "SYSTEM_STT" ||
    configuration.projectRef
  )
    throw new Error("Invalid effective STT configuration");
  return { effective, name: configuration.name };
}
export async function prepareSttActivation(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
  signal: AbortSignal,
): Promise<SttActivationPlan> {
  const fresh = (await history(configuration.ref, signal)).configuration;
  if (
    fresh.ref !== configuration.ref ||
    fresh.kind !== "SYSTEM_STT" ||
    fresh.projectRef ||
    fresh.managedBy !== "UI" ||
    fresh.version !== configuration.version ||
    revision.state !== "PUBLISHED"
  )
    throw new Error("STT activation scope changed");
  // Это чтение одновременно проверяет право управления целевой конфигурацией.
  const target = checkedSttImpact(
    await impact(fresh, revision, signal),
    fresh.ref,
    revision.ref,
  );
  const current = await readEffectiveStt(signal);
  let consumer: ManagedConfigurationConsumerInput = {
    kind: "STT_SERVICE",
    ref: "stt-tts-service",
    expectedAbsent: true,
  };
  let currentName: string | undefined;
  if (current) {
    const source = (await history(current.configurationRef, signal))
      .configuration;
    if (
      source.kind !== "SYSTEM_STT" ||
      source.projectRef ||
      source.ref !== current.configurationRef
    )
      throw new Error("Invalid current STT configuration");
    const page = checkedSttImpact(
      (
        await unwrap(
          sdk.getManagedConfigurationImpact({
            path: {
              configurationRef: source.ref,
              revisionRef: current.revisionRef,
            },
            query: { pageSize: 40 },
            signal: requestSignal(signal),
          }),
        )
      ).data,
      source.ref,
      current.revisionRef,
    );
    const binding = page.consumers[0];
    if (!binding || binding.revisionRef !== current.revisionRef)
      throw new Error("STT binding changed during preparation");
    consumer = { ...binding, expectedAbsent: false };
    currentName = source.name;
  } else if (target.total !== 0)
    throw new Error("STT binding absence mismatch");
  return {
    configuration: fresh,
    revision,
    consumer,
    digest: target.digest,
    current,
    currentName,
  };
}
export async function activateStt(
  plan: SttActivationPlan,
  key: string,
  signal: AbortSignal,
): Promise<void> {
  await mutate(
    (headers) =>
      sdk.rebindSystemSttConsumers({
        path: {
          configurationRef: plan.configuration.ref,
          revisionRef: plan.revision.ref,
        },
        headers: { ...headers, "If-Match": etag(plan.configuration.version) },
        body: { impactDigest: plan.digest, consumers: [plan.consumer] },
        signal: requestSignal(signal),
      }),
    plan.configuration.version,
    key,
  );
}
