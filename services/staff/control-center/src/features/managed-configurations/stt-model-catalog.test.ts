import { describe, expect, it } from "vitest";
import type { SttModelCatalog } from "@/shared/api/generated/openapi/types.gen";
import {
  checkedSttCatalog,
  sttChunkingStrategies,
  sttFormLimits,
  sttParameterSupported,
} from "./stt-model-catalog";

const catalog: SttModelCatalog = {
  version: "catalog-fixture-1",
  observedAt: "2026-09-05T00:00:00Z",
  recommendedModel: "fixture-model",
  recommendedMaximumAudioBytes: 10485760,
  recommendedMaximumAudioDurationMilliseconds: 120000,
  responseFormat: "json",
  models: [
    {
      model: "fixture-model",
      legacy: false,
      parameterNames: ["languages", "chunking_strategy"],
      chunkingStrategies: ["", "auto", "future-mode"],
      fileStreamSupported: true,
      streamEnabled: false,
      maximumPromptBytes: 896,
      maximumKeywords: 64,
      maximumKeywordBytes: 128,
      minimumTemperature: 0,
      maximumTemperature: 1,
    },
  ],
};
describe("STT catalog form boundary", () => {
  it("связывает имена adapter catalog с текущим DTO без расширения допустимых значений", () => {
    const profile = checkedSttCatalog(catalog).models[0];
    expect(sttParameterSupported(profile, "chunkingStrategy")).toBe(true);
    expect(sttParameterSupported(profile, "language")).toBe(false);
    expect(sttParameterSupported(undefined, "languages")).toBe(false);
    expect(sttChunkingStrategies(profile)).toEqual(["", "auto"]);
  });
  it("отличает рекомендации от границ producer и отклоняет неоднозначную модель", () => {
    expect(
      sttFormLimits.find(
        (item) => item.key === "maximumAudioDurationMilliseconds",
      )?.max,
    ).toBe(1800000);
    expect(
      sttFormLimits.find((item) => item.key === "providerTimeoutMilliseconds")
        ?.max,
    ).toBe(15000);
    expect(() =>
      checkedSttCatalog({
        ...catalog,
        models: [...catalog.models, ...catalog.models],
      }),
    ).toThrow("Invalid STT model catalog");
    expect(() =>
      checkedSttCatalog({ ...catalog, recommendedModel: "absent" }),
    ).toThrow("Invalid STT model catalog");
    expect(catalog.recommendedMaximumAudioDurationMilliseconds).toBe(120000);
  });
});
