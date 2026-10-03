import { describe, expect, it, vi } from "vitest";
import {
  createScopedRuntimeSecretCatalog,
  type ScopedSecretCatalogReader,
} from "./scoped-secret-catalog";

const scope = { kind: "ORGANIZATION", organizationRef: "org_alpha" } as const;
const secret: Awaited<ReturnType<ScopedSecretCatalogReader["read"]>> = {
  ref: "secret_alpha",
  version: 2,
  projectRef: "",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_alpha",
  name: "GIT_TOKEN",
  description: "Для работы с репозиторием",
  valueType: "STRING",
  state: "ACTIVE",
  currentRevision: 1,
  nextActions: ["OPEN"],
  createdAt: "2026-10-04T00:00:00Z",
  updatedAt: "2026-10-04T00:00:00Z",
};
function reader() {
  return {
    list: vi
      .fn<ScopedSecretCatalogReader["list"]>()
      .mockResolvedValue({ items: [secret], nextPageToken: "" }),
    read: vi.fn<ScopedSecretCatalogReader["read"]>().mockResolvedValue(secret),
  };
}
const signal = () => new AbortController().signal;
describe("Каталог метаданных секретов для runtime", () => {
  it("убирает посторонние поля материала секрета из ответа каталога", async () => {
    const api = reader();
    api.read.mockResolvedValue({
      ...secret,
      ...{ value: "synthetic-material", sealedValue: "synthetic-sealed" },
    });
    const value = await createScopedRuntimeSecretCatalog("org_alpha", api).read(
      scope,
      secret.ref,
      signal(),
    );
    expect(value).toEqual(secret);
    expect(JSON.stringify(value)).not.toContain("synthetic-material");
  });
  it("не читает чужую организацию", async () => {
    const api = reader();
    await expect(
      createScopedRuntimeSecretCatalog("org_alpha", api).read(
        { ...scope, organizationRef: "org_beta" },
        secret.ref,
        signal(),
      ),
    ).rejects.toThrow("scope mismatch");
    expect(api.read).not.toHaveBeenCalled();
  });
  it.each([
    { ...secret, organizationRef: "org_beta" },
    { ...secret, scopeKind: "PROJECT" as const, projectRef: "prj_alpha" },
    { ...secret, scopeKind: "UNSPECIFIED" as const },
  ])("не смешивает точные owner/scope", async (invalid) => {
    const api = reader();
    api.list.mockResolvedValue({ items: [invalid], nextPageToken: "" });
    await expect(
      createScopedRuntimeSecretCatalog("org_alpha", api).loadPage(
        scope,
        "",
        undefined,
        signal(),
      ),
    ).rejects.toThrow("scope mismatch");
  });
  it("не принимает другую ссылку в exact read", async () => {
    const api = reader();
    api.read.mockResolvedValue({ ...secret, ref: "secret_beta" });
    await expect(
      createScopedRuntimeSecretCatalog("org_alpha", api).read(
        scope,
        secret.ref,
        signal(),
      ),
    ).rejects.toThrow("reference mismatch");
  });
});
