import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { Page, Route } from "@playwright/test";
import type { SystemAssistant } from "../src/shared/api/generated/openapi/types.gen";
import {
  checkFileSelection,
  fileSelectionInitialItems,
} from "./fixtures/file-selection";
import { checkHomeResults, homeResultArtifacts } from "./fixtures/home-results";
import { checkAssistantHistory } from "./fixtures/assistant-history";
import { checkResumableSessions } from "./fixtures/resumable-sessions";
import type { SyntheticSnapshotReader } from "./fixtures/runs-catalog";
import {
  mobileJourneyParts,
  syntheticJourneyProfiles,
  includesJourneySection,
  expectedJourneyDiagnostics,
} from "./synthetic-journey-profile";

describe("bounded synthetic mobile journey", () => {
  it("сохраняет пять ALL journeys и покрывает каждую mobile секцию ровно один раз", () => {
    const profiles = syntheticJourneyProfiles();
    expect(
      profiles
        .filter((profile) => profile.part === "ALL")
        .map((profile) => profile.width),
    ).toEqual([2900, 2560, 1920, 1280, 900]);
    const mobile = profiles.filter((profile) => profile.width === 390);
    expect(mobile.map((profile) => profile.part)).toEqual([
      ...mobileJourneyParts,
    ]);
    for (const section of mobileJourneyParts) {
      expect(
        mobile.filter((profile) =>
          includesJourneySection(profile.part, section),
        ),
      ).toHaveLength(1);
      expect(includesJourneySection("ALL", section)).toBe(true);
    }
    expect(mobile.every((profile) => profile.height === 844)).toBe(true);
  });
  it("разбивает 1440 на пять секций, сохраняя speech lifecycle ровно в CORE_ENV", () => {
    const desktop = syntheticJourneyProfiles().filter(
      (profile) => profile.width === 1440,
    );
    expect(desktop.map((profile) => profile.part)).toEqual([
      ...mobileJourneyParts,
    ]);
    expect(desktop.every((profile) => profile.height === 900)).toBe(true);
    for (const section of mobileJourneyParts)
      expect(
        desktop.filter((profile) =>
          includesJourneySection(profile.part, section),
        ),
      ).toHaveLength(1);
    const source = readFileSync(
      new URL("./synthetic.spec.ts", import.meta.url),
      "utf8",
    );
    expect(source).toContain(
      'if (width === 1440 && includes("CORE_ENV")) await page.clock.install()',
    );
    expect(source).toContain('if (width === 1440 && includes("CORE_ENV")) {');
    expect(source).toContain('reason: "STT_PERMISSION_DENIED"');
    expect(source).toContain("expect(grants).toBe(grantReads)");
  });
  it("разбивает 768 на пять непустых секций без изменения остальных размеров", () => {
    const profiles = syntheticJourneyProfiles();
    const narrow = profiles.filter((profile) => profile.width === 768);
    expect(narrow.map((profile) => profile.part)).toEqual([
      ...mobileJourneyParts,
    ]);
    expect(narrow.every((profile) => profile.height === 1024)).toBe(true);
    expect(narrow.map((profile) => profile.title)).toEqual(
      mobileJourneyParts.map((part) => `synthetic: tablet768 ${part}`),
    );
    for (const section of mobileJourneyParts)
      expect(
        narrow.filter((profile) =>
          includesJourneySection(profile.part, section),
        ),
      ).toHaveLength(1);
    expect(profiles.find((profile) => profile.width === 1920)?.part).toBe(
      "ALL",
    );
    expect(
      mobileJourneyParts.map((part) => expectedJourneyDiagnostics(part, 768)),
    ).toEqual([
      { snapshotConflicts: 0, publicationTimeouts: 2 },
      { snapshotConflicts: 0, publicationTimeouts: 0 },
      { snapshotConflicts: 0, publicationTimeouts: 0 },
      { snapshotConflicts: 0, publicationTimeouts: 0 },
      { snapshotConflicts: 1, publicationTimeouts: 0 },
    ]);
  });
  it("явно расширяет 768 file/history coverage, сохраняя проверки и исключение микрофона", () => {
    const source = readFileSync(
      new URL("./synthetic.spec.ts", import.meta.url),
      "utf8",
    );
    const begin = source.indexOf('if (includes("FILES_HISTORY")) {');
    const end = source.indexOf("const diagnostics =", begin);
    expect(begin).toBeGreaterThan(0);
    expect(end).toBeGreaterThan(begin);
    const files = source.slice(begin, end);
    for (const name of [
      "checkFileSelection",
      "checkAssistantHistory",
      "checkHomeResults",
      "checkResumableSessions",
    ]) {
      const call = files.indexOf(`await ${name}(`);
      expect(call).toBeGreaterThan(0);
      expect(files.slice(files.lastIndexOf("if (", call), call)).toContain(
        "width === 768",
      );
    }
    expect(source).toContain(
      "if (width === 1440 || width === 390)\n      await prepareSyntheticMicrophone(",
    );
    expect(source).toContain("expect(failures).toEqual([])");
    expect(source).toContain("test.setTimeout(120_000)");
  });
  it("создаёт fresh descriptors без разделяемого изменяемого состояния", () => {
    const first = syntheticJourneyProfiles();
    const descriptor = first[0];
    if (!descriptor) throw new Error("Missing synthetic journey descriptor");
    descriptor.width = 1;
    expect(syntheticJourneyProfiles()[0]?.width).toBe(2900);
  });
  it("сохраняет точные negative counts в соответствующих секциях", () => {
    expect(
      mobileJourneyParts.map((part) => expectedJourneyDiagnostics(part, 390)),
    ).toEqual([
      { snapshotConflicts: 0, publicationTimeouts: 2 },
      { snapshotConflicts: 0, publicationTimeouts: 0 },
      { snapshotConflicts: 0, publicationTimeouts: 0 },
      { snapshotConflicts: 0, publicationTimeouts: 0 },
      { snapshotConflicts: 1, publicationTimeouts: 0 },
    ]);
    expect(expectedJourneyDiagnostics("ALL", 2900)).toEqual({
      snapshotConflicts: 1,
      publicationTimeouts: 2,
    });
    expect(expectedJourneyDiagnostics("ALL", 1920)).toEqual({
      snapshotConflicts: 0,
      publicationTimeouts: 2,
    });
  });
  it("каждый test сохраняет бюджет, общий factory и финальные Console/Network guards", () => {
    const source = readFileSync(
      new URL("./synthetic.spec.ts", import.meta.url),
      "utf8",
    );
    expect(source).toContain("syntheticJourneyProfiles()");
    expect(source).toContain("test.setTimeout(120_000)");
    expect(source).toContain("expect(failures).toEqual([])");
    expect(source).toContain(
      "for (const { request, changedRoute, code } of failedRequests)",
    );
    expect(source).toContain(
      'if (!includes("CORE_ENV")) releaseInitialHomeCatalogs()',
    );
    expect(source).toContain('if (part === "WORKFLOW_CONTEXT")');
    const renewal = source.indexOf(
      "await expect.poll(() => sessionRenewals).toBe(1)",
    );
    const resume = source.indexOf(
      "await expect.poll(() => sessionResumes).toBe(2)",
    );
    const settled = source.indexOf('if (!includes("CORE_ENV")) {');
    const core = source.indexOf('if (includes("CORE_ENV")) {');
    expect(renewal).toBeGreaterThan(0);
    expect(resume).toBeGreaterThan(renewal);
    expect(settled).toBeGreaterThan(resume);
    expect(core).toBeGreaterThan(settled);
    expect(source).toContain(
      'if (part === "PROVIDERS_INTEGRATIONS") await installIntegrationBase()',
    );
    for (const name of [
      "checkFileSelection",
      "checkAssistantHistory",
      "checkHomeResults",
      "checkResumableSessions",
    ])
      expect(source).toContain(`await ${name}(`);
  });
  it("сохраняет settled workflow unmount и текущий allSources read без ослабления selection/OCC", () => {
    const workflow = readFileSync(
      new URL("./fixtures/workflow.ts", import.meta.url),
      "utf8",
    );
    const leave = workflow.indexOf('await page.goto("/")');
    const settled = workflow.indexOf(
      ".home-project-section .home-project",
      leave,
    );
    const publish = workflow.indexOf('workflow.state = "PUBLISHED"', leave);
    expect(settled).toBeGreaterThan(leave);
    expect(publish).toBeGreaterThan(settled);
    const files = readFileSync(
      new URL("./fixtures/file-selection.ts", import.meta.url),
      "utf8",
    );
    expect(files).toContain('expect(query.getAll("sourceKinds")).toEqual([])');
    expect(files).toContain('expect(query.get("query") ?? "").toBe("")');
    expect(files).toContain(
      'expect(query.get("lifecycleState")).toBe("ACTIVE")',
    );
    expect(files).toContain(
      'expect(route.request().headers()["if-match"]).toBe(\'"4"\')',
    );
    expect(files).toContain("expect(agentReadCalls).toBe(0)");
    expect(files).toContain("expect(bindingCursorCalls).toBe(1)");
  });
  it("allSources HTTP mirror сохраняет own items и action eligibility", async () => {
    const stop = new Error("Synthetic navigation boundary");
    const handlers = new Map<string, (route: Route) => Promise<void>>();
    const page = {
      route: (pattern: string, handler: (route: Route) => Promise<void>) =>
        handlers.set(pattern, handler),
      goto: () => {
        throw stop;
      },
    } as unknown as Page;
    await expect(
      checkFileSelection(page, "project_owned", async () => {}),
    ).rejects.toBe(stop);
    const read = handlers.get("**/api/v1/projects/project_owned/artifacts*");
    if (!read) throw new Error("Missing owned artifact mirror");
    let result:
      | {
          items: ReturnType<typeof fileSelectionInitialItems>;
          total: number;
          nextPageToken: string;
        }
      | undefined;
    await read({
      request: () => ({
        url: () =>
          "https://kodex.test/api/v1/projects/project_owned/artifacts?pageSize=8&lifecycleState=ACTIVE",
      }),
      fulfill: ({ json }: { json: typeof result }) => {
        result = json;
      },
    } as unknown as Route);
    if (!result) throw new Error("Missing owned artifact page");
    expect(result.items.map((item) => item.ref)).toEqual([
      "artifact_selection",
      "artifact_no_delete",
    ]);
    expect(
      result.items.every((item) => item.projectRef === "project_owned"),
    ).toBe(true);
    expect(result.items[0]?.nextActions).toEqual(["DELETE", "BIND"]);
    expect(result.items[1]?.nextActions).toEqual([]);
    expect(result.total).toBe(7);
    expect(result.nextPageToken).toBe("files-next");
  });
  it("Home и session producers читают тот же mutable HTTP state и закрывают foreign scope", async () => {
    const stop = new Error("Synthetic navigation boundary");
    const handlers = new Map<string, (route: Route) => Promise<void>>();
    const readers = new Map<string, SyntheticSnapshotReader>();
    const page = {
      route: (pattern: string, handler: (route: Route) => Promise<void>) =>
        handlers.set(pattern, handler),
      goto: () => {
        throw stop;
      },
    } as unknown as Page;
    const install = (kind: string, read: SyntheticSnapshotReader) =>
      readers.set(kind, read);
    await expect(
      checkHomeResults(page, "project_owned", async () => {}, install),
    ).rejects.toBe(stop);
    const homeRead = readers.get("ARTIFACT");
    if (!homeRead) throw new Error("Missing Home artifact snapshot");
    expect(homeRead("project_foreign")).toBeUndefined();
    expect(homeRead()?.catalog?.artifacts).toHaveLength(6);
    expect(
      homeRead("project_owned")?.catalog?.artifacts?.every(
        (item) => item.projectRef === "project_owned",
      ),
    ).toBe(true);
    expect(homeResultArtifacts("project_owned")).toHaveLength(41);
    const homeHttp = handlers.get(
      "**/api/v1/projects/project_owned/artifacts?**",
    );
    if (!homeHttp) throw new Error("Missing owned Home artifact mirror");
    let filteredItems: unknown;
    await homeHttp({
      request: () => ({
        url: () =>
          "https://kodex.test/api/v1/projects/project_owned/artifacts?pageSize=6&lifecycleState=ACTIVE&query=%D0%9B%D0%B8%D1%87%D0%BD%D1%8B%D0%B9%20%D1%80%D0%B5%D0%B7%D1%83%D0%BB%D1%8C%D1%82%D0%B0%D1%82%201&sourceKinds=AGENT_RESULT&sourceKinds=INTEGRATION_RESULT",
      }),
      fulfill: ({ json }: { json: { items: unknown } }) => {
        filteredItems = json.items;
      },
    } as unknown as Route);
    expect(filteredItems).toEqual([homeResultArtifacts("project_owned")[1]]);
    const copy = homeRead();
    if (!copy?.catalog?.artifacts?.[0])
      throw new Error("Missing Home artifact");
    copy.catalog.artifacts[0].fileName = "forged";
    expect(homeRead()?.catalog?.artifacts?.[0]?.fileName).not.toBe("forged");
    await expect(
      checkResumableSessions(page, "project_owned", async () => {}, install),
    ).rejects.toBe(stop);
    const runRead = readers.get("RUN");
    const http = handlers.get("**/api/v1/runs?**");
    if (!runRead || !http) throw new Error("Missing continuation producer");
    expect(runRead("project_foreign")).toBeUndefined();
    expect(runRead("project_owned")?.catalog?.runs).toHaveLength(30);
    let sessionPrefix:
      | { items: { title: string }[]; total: number; nextPageToken: string }
      | undefined;
    await http({
      request: () => ({
        url: () =>
          "https://kodex.test/api/v1/runs?resumableSessionsOnly=true&pageSize=6&projectRef=project_owned&targetType=AGENT&targetRef=agent_session_catalog",
      }),
      fulfill: ({ json }: { json: typeof sessionPrefix }) => {
        sessionPrefix = json;
      },
    } as unknown as Route);
    if (!sessionPrefix) throw new Error("Missing session prefix");
    expect(sessionPrefix.items).toHaveLength(6);
    expect(sessionPrefix.items[0]?.title).toBe("Продолжение 0");
    expect(sessionPrefix.total).toBe(43);
    expect(sessionPrefix.nextPageToken).toBe("session-snapshot");
    await http({
      request: () => ({
        url: () =>
          "https://kodex.test/api/v1/runs?resumableSessionsOnly=true&pageSize=6&pageToken=session-snapshot",
      }),
      fulfill: ({ status }: { status: number }) => {
        expect(status).toBe(412);
      },
    } as unknown as Route);
    expect(
      runRead("project_owned")?.catalog?.runs?.map((run) => run.ref),
    ).toEqual(["run_catalog_50"]);
    expect(runRead()?.catalog?.runs?.[0]?.nextActions).toEqual([
      "OPEN",
      "ADD_TURN",
    ]);
  });
  it("история использует exact assistant pin, current archived read и SharedConfirmation", async () => {
    const stop = new Error("Synthetic navigation boundary");
    const handlers = new Map<string, (route: Route) => Promise<void>>();
    const readers = new Map<string, SyntheticSnapshotReader>();
    const assistant: SystemAssistant = {
      ref: "assistant_owned",
      version: 1,
      name: "Kodex",
      system: true,
      removable: false,
      corePromptRevision: "1",
      ownerInstructions: "",
      runtimeState: "READY",
      readinessSummary: "",
      nextActions: [],
    };
    const page = {
      route: (pattern: string, handler: (route: Route) => Promise<void>) =>
        handlers.set(pattern, handler),
      goto: () => {
        throw stop;
      },
    } as unknown as Page;
    await expect(
      checkAssistantHistory(page, "project_owned", assistant, (kind, read) =>
        readers.set(kind, read),
      ),
    ).rejects.toBe(stop);
    const read = readers.get("SYSTEM_ASSISTANT");
    const archive = handlers.get(
      "**/api/v1/assistant-conversations/cnv_older/archive",
    );
    const list = handlers.get("**/api/v1/assistant-conversations?**");
    if (!read || !archive || !list)
      throw new Error("Missing owned assistant history");
    expect(read("project_foreign")).toBeUndefined();
    expect(
      read("project_owned")?.conversations?.conversations?.[0]?.assistantRef,
    ).toBe(assistant.ref);
    await archive({
      request: () => ({
        method: () => "POST",
        headers: () => ({
          "if-match": '"1"',
          "idempotency-key": "fixture",
          "x-csrf-token": "fixture",
        }),
      }),
      fulfill: () => {},
    } as unknown as Route);
    let result: unknown;
    await list({
      request: () => ({
        url: () =>
          "https://kodex.test/api/v1/assistant-conversations?projectRef=project_owned&assistantScope=SYSTEM&pageSize=6&state=ARCHIVED&query=Предыдущий",
      }),
      fulfill: ({ json }: { json: unknown }) => {
        result = json;
      },
    } as unknown as Route);
    expect(result).toMatchObject({
      items: [
        {
          ref: "cnv_older",
          projectRef: "project_owned",
          assistantRef: assistant.ref,
          state: "ARCHIVED",
          version: 2,
        },
      ],
    });
    const source = readFileSync(
      new URL("./fixtures/assistant-history.ts", import.meta.url),
      "utf8",
    );
    expect(source).not.toContain('page.once("dialog"');
    expect(source).toContain(".confirmation-dialog");
  });
});
