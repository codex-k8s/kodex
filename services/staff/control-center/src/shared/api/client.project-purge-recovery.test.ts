import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureApiClient } from "./client";
import { client } from "./generated/openapi/client.gen";
import { getProjectPurgeRecovery } from "./generated/openapi/sdk.gen";
import { selectedProjectRef, selectProjectRef } from "@/shared/project-context";

vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({
    apiBaseUrl: "https://kodex.example",
    requestTimeoutMs: 3000,
  }),
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("./document-lifetime", () => ({
  installDocumentRequestLifetime: vi.fn(),
  documentFetch: (request: Request) => fetch(request),
  retainRequestSignalParents: (request: Request) => request,
}));
beforeEach(() => {
  selectProjectRef("prj_synthetic");
  configureApiClient();
});
afterEach(() => {
  selectProjectRef();
  vi.unstubAllGlobals();
});

describe("Organization boundary восстановления удаления", () => {
  it("generated GET не передаёт project header даже при том же выбранном проекте", async () => {
    const requests: Request[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((request: Request) => {
        requests.push(request);
        return Promise.resolve(
          new Response("{}", {
            headers: { "Content-Type": "application/json" },
          }),
        );
      }),
    );
    await getProjectPurgeRecovery({ path: { projectRef: "prj_synthetic" } });
    expect(requests[0]?.headers.has("X-Kodex-Project-ID")).toBe(false);
    expect(requests[0]?.headers.get("Accept-Language")).toBe("ru");
    expect(selectedProjectRef()).toBe("prj_synthetic");
  });

  it("удаляет явный project header только с exact recovery path", async () => {
    const requests: Request[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((request: Request) => {
        requests.push(request);
        return Promise.resolve(
          new Response("{}", {
            headers: { "Content-Type": "application/json" },
          }),
        );
      }),
    );
    await client.post({
      url: "/api/v1/projects/prj_synthetic/purge-recovery",
      headers: { "X-Kodex-Project-ID": "prj_other" },
    });
    await client.get({ url: "/api/v1/projects/prj_synthetic" });
    expect(requests[0]?.headers.has("X-Kodex-Project-ID")).toBe(false);
    expect(requests[1]?.headers.get("X-Kodex-Project-ID")).toBe(
      "prj_synthetic",
    );
  });
});
