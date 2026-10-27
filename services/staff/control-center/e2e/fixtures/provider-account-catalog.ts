import { expect } from "@playwright/test";
import type {
  ModelCapabilityPage,
  ProviderAccountCandidateInput,
} from "../../src/shared/api/generated/openapi/types.gen";

// Проверяет ограниченную последовательность страниц точного каталога и возвращает
// закреплённого кандидата; способ чтения и вызов изменения остаются у сценария.
export async function selectProviderAccountCandidate(
  accountRef: string,
  modelId: string,
  readPage: (path: string) => Promise<ModelCapabilityPage>,
): Promise<ProviderAccountCandidateInput> {
  const query = new URLSearchParams({
    providerAccountRef: accountRef,
    query: modelId,
    pageSize: "100",
  });
  let catalogPin = "";
  const cursors = new Set<string>();
  for (let index = 0; index < 20; index += 1) {
    const catalog = await readPage(`/api/v1/model-capabilities?${query}`);
    expect(catalog.catalogStatus?.state).toBe("READY");
    expect(Date.parse(catalog.catalogStatus?.expiresAt ?? "")).toBeGreaterThan(
      Date.now(),
    );
    expect(catalog.catalogRevision).toMatch(/^mcat_[a-f0-9]{64}$/);
    expect(catalog.catalogDigest).toMatch(/^[a-f0-9]{64}$/);
    const pin = `${catalog.catalogRevision}:${catalog.catalogDigest}`;
    if (catalogPin) expect(pin).toBe(catalogPin);
    catalogPin = pin;
    const model = catalog.items.find((item) => item.id === modelId);
    if (model) {
      expect(model.available).toBe(true);
      expect(model.eligibleProviderAccountRefs).toContain(accountRef);
      return {
        accountRef,
        weight: 1,
        catalogRevision: catalog.catalogRevision,
        catalogDigest: catalog.catalogDigest,
        providerDefinitionKey: model.providerDefinitionKey,
      };
    }
    if (!catalog.nextPageToken) break;
    expect(cursors.has(catalog.nextPageToken)).toBe(false);
    cursors.add(catalog.nextPageToken);
    query.set("pageToken", catalog.nextPageToken);
  }
  throw new Error("Selected model is absent from the exact account catalog");
}
