import { getSystemSttModelCatalog } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  SttModelCatalog,
  SttModelProfile,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

// Границы SystemSTTSpecification из исходного OpenAPI; рекомендации каталога их не заменяют.
export const sttFormLimits = [
  { key: "maximumAudioBytes", min: 1024, max: 26214400 },
  { key: "maximumAudioDurationMilliseconds", min: 1000, max: 1800000 },
  { key: "providerTimeoutMilliseconds", min: 1000, max: 15000 },
] as const;

export function sttParameterSupported(
  profile: SttModelProfile | undefined,
  key: string,
): boolean {
  return (
    profile?.parameterNames.includes(
      key === "chunkingStrategy" ? "chunking_strategy" : key,
    ) ?? false
  );
}

export function sttChunkingStrategies(
  profile: SttModelProfile | undefined,
): string[] {
  return (
    profile?.chunkingStrategies.filter(
      (value) => value === "" || value === "auto",
    ) ?? []
  );
}

export function checkedSttCatalog(value: SttModelCatalog): SttModelCatalog {
  if (
    !value.version ||
    !Number.isFinite(Date.parse(value.observedAt)) ||
    !Array.isArray(value.models) ||
    value.models.length < 1 ||
    value.models.length > 128 ||
    new Set(value.models.map((item) => item.model)).size !==
      value.models.length ||
    !value.models.some((item) => item.model === value.recommendedModel) ||
    value.models.some(
      (item) =>
        !item.model ||
        !Array.isArray(item.parameterNames) ||
        !Array.isArray(item.chunkingStrategies) ||
        !Number.isFinite(item.minimumTemperature) ||
        !Number.isFinite(item.maximumTemperature) ||
        item.minimumTemperature > item.maximumTemperature,
    )
  )
    throw new Error("Invalid STT model catalog");
  return value;
}

export async function readSttCatalog(
  signal: AbortSignal,
): Promise<SttModelCatalog> {
  return checkedSttCatalog(
    (
      await unwrap(
        getSystemSttModelCatalog({
          signal: AbortSignal.any([signal, requestSignal()]),
          cache: "no-store",
        }),
      )
    ).data,
  );
}
