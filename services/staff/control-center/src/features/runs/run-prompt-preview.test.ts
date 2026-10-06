import { beforeEach, expect, it, vi } from "vitest";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

const sdk = vi.hoisted(() => ({
  previewPromptTemplate: vi.fn(),
  queryPromptTemplateVariables: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/mutation", () => ({ csrfToken: () => "c".repeat(43) }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { loadRunPromptPreview } from "./run-prompt-preview";

const preview: PromptTemplatePreview = {
  safePreview: "[protected input]",
  complete: true,
  diagnostics: [],
  templateRef: "preview_example",
  templateDigest: "a".repeat(64),
  materializationDigest: "b".repeat(64),
  effectiveCapabilities: [],
  serviceTemplateRevision: "prompt.v1",
  serviceTemplateDigest: "c".repeat(64),
  variableSnapshotDigest: "d".repeat(64),
  locale: "ru",
  slots: [{ source: "PLATFORM", slot: "INPUT", position: 1 }],
  sections: [
    { source: "PLATFORM", slot: "INPUT", content: "[protected input]" },
  ],
  contextPin: { digest: "e".repeat(64) },
};
function respond(data: unknown): void {
  sdk.previewPromptTemplate.mockResolvedValue({
    data,
    response: new Response(null),
  });
}
beforeEach(() => {
  vi.resetAllMocks();
  respond(preview);
});

it("читает сохранённый RUN напрямую с пустым шаблоном и без полного текста/каталога", async () => {
  const signal = new AbortController().signal;
  const result = await loadRunPromptPreview("run_example", signal);
  expect(result).toEqual(preview);
  expect(sdk.queryPromptTemplateVariables).not.toHaveBeenCalled();
  expect(sdk.previewPromptTemplate).toHaveBeenCalledExactlyOnceWith({
    body: {
      template: "",
      targetKind: "RUN",
      targetRef: "run_example",
      includeFullMaterialization: false,
    },
    headers: { "X-CSRF-Token": "c".repeat(43) },
    signal,
    cache: "no-store",
  });
});

it.each([
  { ...preview, fullMaterializedPrompt: "PRIVATE_FULL_PROMPT" },
  { ...preview, templateDigest: "invalid" },
  { ...preview, templateRef: undefined },
  { ...preview, contextPin: undefined },
  { ...preview, sections: [] },
  { ...preview, sections: [{ source: "FOREIGN", content: "safe" }] },
  { ...preview, slots: [{ source: "PLATFORM", slot: "FOREIGN", position: 1 }] },
])("закрыто отвергает full disclosure или повреждённые pins", async (value) => {
  respond(value);
  await expect(
    loadRunPromptPreview("run_example", new AbortController().signal),
  ).rejects.toThrow("Invalid safe run prompt preview boundary");
});

it("не копирует неизвестные поля секций и контекста", async () => {
  respond({
    ...preview,
    privatePayload: "PRIVATE_PAYLOAD",
    sections: [{ ...preview.sections[0], privatePayload: "PRIVATE_PAYLOAD" }],
    contextPin: { ...preview.contextPin, privatePayload: "PRIVATE_PAYLOAD" },
  });
  const result = await loadRunPromptPreview(
    "run_example",
    new AbortController().signal,
  );
  expect(JSON.stringify(result)).not.toContain("PRIVATE_PAYLOAD");
});

it("сохраняет безопасную секцию длиннее 256 символов без усечения или подмены", async () => {
  const sections = [{ ...preview.sections[0], content: "s".repeat(1024) }];
  respond({ ...preview, sections });
  const result = await loadRunPromptPreview(
    "run_example",
    new AbortController().signal,
  );
  expect(result.sections).toEqual(sections);
});

it("сохраняет авторитетные диагностику и признак неполноты безопасной проекции", async () => {
  const diagnostics: PromptTemplatePreview["diagnostics"] = [
    {
      code: "CAPABILITY_REQUIRED",
      message: "Safe diagnostic",
      severity: "ERROR",
      line: 1,
      column: 2,
    },
  ];
  respond({ ...preview, complete: false, diagnostics });
  const result = await loadRunPromptPreview(
    "run_example",
    new AbortController().signal,
  );
  expect(result.complete).toBe(false);
  expect(result.diagnostics).toEqual(diagnostics);
});

it("сохраняет закрытый forbidden/fresh-auth результат без повторного запроса или полного текста", async () => {
  const problem = new AppProblem({
    status: 403,
    code: "FRESH_AUTHENTICATION_REQUIRED",
    kind: "forbidden",
    retryable: false,
  });
  sdk.previewPromptTemplate.mockRejectedValue(problem);
  await expect(
    loadRunPromptPreview("run_example", new AbortController().signal),
  ).rejects.toBe(problem);
  expect(sdk.previewPromptTemplate).toHaveBeenCalledTimes(1);
});

it("не принимает поздний ACK закрытого контекста", async () => {
  let complete!: (value: unknown) => void;
  sdk.previewPromptTemplate.mockReturnValue(
    new Promise((resolve) => {
      complete = resolve;
    }),
  );
  const controller = new AbortController();
  const result = loadRunPromptPreview("run_example", controller.signal);
  controller.abort();
  complete({ data: preview, response: new Response(null) });
  await expect(result).rejects.toMatchObject({ name: "AbortError" });
});

it("не начинает запрос с закрытым signal или неверным ref", async () => {
  const controller = new AbortController();
  controller.abort();
  await expect(
    loadRunPromptPreview("run_example", controller.signal),
  ).rejects.toMatchObject({ name: "AbortError" });
  await expect(
    loadRunPromptPreview("wrong/ref", new AbortController().signal),
  ).rejects.toThrow("Invalid run prompt preview target");
  expect(sdk.previewPromptTemplate).not.toHaveBeenCalled();
});
