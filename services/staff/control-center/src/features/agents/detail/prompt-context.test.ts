import { beforeEach, expect, it, vi } from "vitest";
import type {
  PromptContextPin,
  PromptTemplatePreview,
  TemplateVariablePage,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  catalog: vi.fn(),
  preview: vi.fn(),
  validate: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  queryPromptTemplateVariables: sdk.catalog,
  previewPromptTemplate: sdk.preview,
  validatePromptTemplate: sdk.validate,
}));
vi.mock("@/shared/api/mutation", () => ({ csrfToken: () => "c".repeat(43) }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  checkedPromptPin,
  createPromptVariableLoader,
  previewContextPrompt,
  validateContextPrompt,
  type PromptTarget,
} from "./prompt-context";
const target: PromptTarget = {
  projectRef: "project",
  targetKind: "AGENT",
  targetRef: "agent",
  context: { expectedAgentVersion: 7, task: "Входной текст" },
};
const pin: PromptContextPin = {
  digest: "a".repeat(64),
  agentRef: "agent",
  agentVersion: 7,
};
const page: TemplateVariablePage = {
  contextPin: pin,
  total: 2,
  nextPageToken: "next",
  items: [
    {
      name: "input.values",
      source: "INPUT",
      valueType: "OBJECT",
      description: "Входные поля",
      example: "{{ .input.values }}",
      available: false,
      reason: "PERMISSION_REQUIRED",
      collection: false,
      itemFields: [],
    },
  ],
};
const preview: PromptTemplatePreview = {
  safePreview: "Безопасный результат",
  complete: true,
  diagnostics: [],
  templateRef: "template",
  templateDigest: "b".repeat(64),
  materializationDigest: "c".repeat(64),
  effectiveCapabilities: [],
  serviceTemplateRevision: "v7",
  serviceTemplateDigest: "d".repeat(64),
  variableSnapshotDigest: "e".repeat(64),
  locale: "ru",
  slots: [{ slot: "INPUT", source: "PLATFORM", position: 0 }],
  sections: [{ slot: "INPUT", source: "PLATFORM", content: "Контекст" }],
  contextPin: pin,
};
beforeEach(() => {
  vi.resetAllMocks();
  sdk.catalog.mockResolvedValue({ data: page, response: new Response(null) });
  sdk.preview.mockResolvedValue({
    data: preview,
    response: new Response(null),
  });
});
it("запрашивает typed context в body, сохраняет disabled metadata и context pin следующих страниц", async () => {
  const load = createPromptVariableLoader(target);
  const signal = new AbortController().signal;
  const first = await load({ query: "input", signal });
  expect(first.items[0]).toMatchObject({
    disabled: true,
    variable: { valueType: "OBJECT", reason: "PERMISSION_REQUIRED" },
  });
  expect(first.total).toBe(2);
  sdk.catalog.mockResolvedValue({
    data: { ...page, nextPageToken: undefined },
    response: new Response(null),
  });
  await load({ query: "input", cursor: "next", signal });
  expect(sdk.catalog).toHaveBeenLastCalledWith({
    body: {
      ...target,
      query: "input",
      pageSize: 50,
      pageToken: "next",
      expectedContextDigest: pin.digest,
    },
    headers: { "X-CSRF-Token": "c".repeat(43) },
    signal,
    cache: "no-store",
  });
});
it("не заменяет изменённый server context на следующей странице", async () => {
  const load = createPromptVariableLoader(target);
  const signal = new AbortController().signal;
  await load({ query: "", signal });
  sdk.catalog.mockResolvedValue({
    data: {
      ...page,
      contextPin: { ...pin, digest: "f".repeat(64) },
      nextPageToken: undefined,
    },
    response: new Response(null),
  });
  await expect(load({ query: "", cursor: "next", signal })).rejects.toThrow(
    "snapshot",
  );
});
it("отклоняет diff чужой Session или прежней ревизии при том же context digest", async () => {
  const continuationPin = { ...pin, previousRuntimeRevisionRef: "previous" };
  sdk.catalog.mockResolvedValue({
    data: { ...page, contextPin: continuationPin },
    response: new Response(null),
  });
  for (const runtimeDiff of [
    {
      sessionRef: "foreign",
      previousRevisionRef: "previous",
      digest: "d".repeat(64),
      changes: [],
    },
    {
      sessionRef: "session",
      previousRevisionRef: "foreign",
      digest: "d".repeat(64),
      changes: [],
    },
  ]) {
    sdk.preview.mockResolvedValue({
      data: { ...preview, contextPin: continuationPin, runtimeDiff },
      response: new Response(null),
    });
    await expect(
      previewContextPrompt(
        { targetKind: "SESSION_CONTINUATION", targetRef: "session" },
        "",
        new AbortController().signal,
      ),
    ).rejects.toThrow("runtime identity");
  }
  sdk.preview.mockResolvedValue({
    data: {
      ...preview,
      contextPin: continuationPin,
      runtimeDiff: {
        sessionRef: "session",
        previousRevisionRef: "previous",
        digest: "d".repeat(64),
        changes: [],
      },
    },
    response: new Response(null),
  });
  await expect(
    previewContextPrompt(
      { targetKind: "SESSION_CONTINUATION", targetRef: "session" },
      "",
      new AbortController().signal,
    ),
  ).resolves.toMatchObject({
    runtimeDiff: { sessionRef: "session", previousRevisionRef: "previous" },
  });
});
it("safe preview использует fresh catalog pin и не запрашивает full permission неявно", async () => {
  const signal = new AbortController().signal;
  await expect(previewContextPrompt(target, "Шаблон", signal)).resolves.toEqual(
    preview,
  );
  expect(sdk.preview).toHaveBeenCalledWith({
    body: {
      template: "Шаблон",
      targetKind: "AGENT",
      targetRef: "agent",
      context: target.context,
      expectedContextDigest: pin.digest,
      includeFullMaterialization: false,
    },
    headers: { "X-CSRF-Token": "c".repeat(43) },
    signal,
    cache: "no-store",
  });
  sdk.preview.mockResolvedValue({
    data: { ...preview, contextPin: { ...pin, agentVersion: 8 } },
    response: new Response(null),
  });
  await expect(previewContextPrompt(target, "Шаблон", signal)).rejects.toThrow(
    "context changed",
  );
});
it("не выдаёт unexpected full text и сохраняет authoritative diagnostics", async () => {
  const signal = new AbortController().signal;
  sdk.preview.mockResolvedValue({
    data: { ...preview, fullMaterializedPrompt: "Полный текст" },
    response: new Response(null),
  });
  await expect(previewContextPrompt(target, "Шаблон", signal)).rejects.toThrow(
    "Unexpected full",
  );
  await expect(
    previewContextPrompt(target, "Шаблон", signal, true),
  ).resolves.toMatchObject({ fullMaterializedPrompt: "Полный текст" });
  sdk.validate.mockResolvedValue({
    data: {
      valid: false,
      diagnostics: [
        {
          code: "CAPABILITY_REQUIRED",
          severity: "ERROR",
          message: "Unavailable",
          line: 1,
          column: 1,
        },
      ],
      contextPin: pin,
    },
    response: new Response(null),
  });
  await expect(
    validateContextPrompt(target, "Шаблон", signal),
  ).resolves.toMatchObject({ valid: false });
});
it("Workflow/continuation требуют server-owned revision identity, не подставляют ordinal или Run.ref", () => {
  const workflow: PromptTarget = {
    targetKind: "WORKFLOW_STAGE",
    targetRef: "workflow",
    context: {
      workflowRevisionRef: "draft",
      workflowStageKey: "step",
      expectedWorkflowVersion: 9,
    },
  };
  expect(() =>
    checkedPromptPin(
      {
        digest: pin.digest,
        workflowRef: "workflow",
        workflowVersion: 9,
        workflowRevisionRef: "other",
        workflowStageKey: "step",
      },
      workflow,
    ),
  ).toThrow();
  expect(() =>
    checkedPromptPin(pin, {
      targetKind: "SESSION_CONTINUATION",
      targetRef: "session",
    }),
  ).toThrow();
});
