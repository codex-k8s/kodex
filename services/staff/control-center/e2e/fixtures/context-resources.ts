import { expect, type Page } from "@playwright/test";
import { installContextBindingFixture } from "./context-bindings";
import type {
  Artifact,
  KodexMemoryRecord,
  MemoryRecordSpecification,
  MemoryRecordCreateInput,
  SkillBundle,
  SkillBundleDraftCreateInput,
  ContextRevisionDigestInput,
  SkillBundleReviewInput,
  RuntimeEnvironmentSet,
} from "../../src/shared/api/generated/openapi/types.gen";
export async function checkContextResources(
  page: Page,
  projectRef: string,
  environment: RuntimeEnvironmentSet,
  capture: (name: string) => Promise<void>,
): Promise<void> {
  const checkBinding = await installContextBindingFixture(
    page,
    projectRef,
    environment,
  );
  const now = "2026-09-05T00:00:00Z";
  const provenance = {
    actorRef: "user_synthetic",
    sourceKind: "USER",
    digest: "a".repeat(64),
    createdAt: now,
  };
  let memory: KodexMemoryRecord | undefined;
  let expiryReads = 0;
  let skill: SkillBundle | undefined;
  const events: string[] = [];
  const artifact: Artifact = {
    ref: "artifact_skill_synthetic",
    version: 9,
    projectRef,
    fileName: "SKILL.md",
    mediaType: "text/markdown",
    sizeBytes: 80,
    digest: `sha256:${"b".repeat(64)}`,
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    revision: 3,
    lifecycleState: "ACTIVE",
    agentBindings: [],
    previewAvailable: true,
    createdAt: now,
    nextActions: [],
  };
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    if (path === "/api/v1/memory-records/memory_expiring") {
      expiryReads += 1;
      await route.fulfill({
        json:
          expiryReads === 1
            ? expiringMemory
            : {
                ...expiringMemory,
                state: "EXPIRED",
                currentRevision: {
                  ...expiringMemory.currentRevision,
                  summary: "",
                  redacted: true,
                },
              },
      });
      return;
    }
    if (
      path === `/api/v1/projects/${projectRef}/artifacts` &&
      method === "POST"
    ) {
      expect(request.headers()["x-file-name"]).toBe("SKILL.md");
      expect(request.postData()).toContain("Synthetic instructions");
      await route.fulfill({
        status: 201,
        json: { ...artifact, scanState: "PENDING" },
      });
      return;
    }
    if (path === `/api/v1/artifacts/${artifact.ref}`) {
      await route.fulfill({ json: artifact });
      return;
    }
    if (path === `/api/v1/projects/${projectRef}/artifacts`) {
      await route.fulfill({ json: { items: [artifact], nextPageToken: "" } });
      return;
    }
    if (
      path === `/api/v1/projects/${projectRef}/memory-records` &&
      method === "POST"
    ) {
      const input = request.postDataJSON() as MemoryRecordCreateInput;
      expect(request.headers()["idempotency-key"]).toBeTruthy();
      expect(request.headers()["if-match"]).toBeUndefined();
      memory = {
        ref: "memory_synthetic",
        version: 1,
        projectRef,
        state: "ACTIVE",
        currentRevision: {
          ...input.specification,
          ref: "memory_revision_1",
          revision: 1,
          digest: "c".repeat(64),
          provenance,
          redacted: false,
        },
        createdAt: now,
        updatedAt: now,
      };
      events.push("memory-create");
      await route.fulfill({ status: 201, json: memory });
      return;
    }
    if (path === "/api/v1/memory-records/memory_synthetic" && memory) {
      await route.fulfill({ json: memory });
      return;
    }
    if (
      path === "/api/v1/memory-records/memory_synthetic/revisions" &&
      memory
    ) {
      if (method === "POST") {
        expect(request.headers()["if-match"]).toBe(
          `"${String(memory.version)}"`,
        );
        const input = request.postDataJSON() as MemoryRecordSpecification;
        memory = {
          ...memory,
          version: memory.version + 1,
          currentRevision: {
            ...memory.currentRevision,
            ...input,
            revision: memory.currentRevision.revision + 1,
            ref: "memory_revision_2",
          },
        };
        events.push("memory-revise");
        await route.fulfill({ status: 201, json: memory });
        return;
      }
      await route.fulfill({
        json: { items: [memory.currentRevision], total: 1, nextPageToken: "" },
      });
      return;
    }
    if (path === "/api/v1/memory-records") {
      await route.fulfill({
        json: {
          items: memory ? [memory] : [],
          total: memory ? 1 : 0,
          nextPageToken: "",
        },
      });
      return;
    }
    if (
      path.startsWith("/api/v1/memory-records/memory_synthetic/") &&
      memory &&
      method === "POST"
    ) {
      expect(request.headers()["if-match"]).toBe(`"${String(memory.version)}"`);
      const action = path.split("/").at(-1);
      if (action === "archive")
        memory = { ...memory, version: memory.version + 1, state: "ARCHIVED" };
      else if (action === "restoration")
        memory = { ...memory, version: memory.version + 1, state: "ACTIVE" };
      else if (action === "purge")
        memory = {
          ...memory,
          version: memory.version + 1,
          state: "PURGED",
          currentRevision: {
            ...memory.currentRevision,
            summary: "",
            redacted: true,
          },
        };
      else {
        await route.fallback();
        return;
      }
      events.push(`memory-${action}`);
      await route.fulfill({ json: memory });
      return;
    }
    if (
      path === `/api/v1/projects/${projectRef}/skill-bundle-drafts` &&
      method === "POST"
    ) {
      const input = request.postDataJSON() as SkillBundleDraftCreateInput;
      expect(input.specification.files).toEqual([
        { path: "SKILL.md", artifactRef: artifact.ref, artifactRevision: 3 },
      ]);
      skill = {
        ref: "skill_synthetic",
        version: 1,
        projectRef,
        state: "ACTIVE",
        createdAt: now,
        updatedAt: now,
        draftRevision: {
          ref: "skill_revision",
          revision: 1,
          state: "DRAFT",
          name: input.specification.name,
          description: input.specification.description,
          files: input.specification.files.map((file) => ({
            ...file,
            digest: artifact.digest,
            sizeBytes: artifact.sizeBytes,
          })),
          digest: "d".repeat(64),
          provenance,
          scanState: "PENDING",
          diagnostics: [],
        },
      };
      events.push("skill-create");
      await route.fulfill({ status: 201, json: skill });
      return;
    }
    if (path === "/api/v1/skill-bundles/skill_synthetic" && skill) {
      await route.fulfill({ json: skill });
      return;
    }
    if (path === "/api/v1/skill-bundles") {
      const query = new URL(request.url()).searchParams;
      expect(query.get("projectRef")).toBe(projectRef);
      const source = skill;
      const published = source?.currentRevision;
      const entries =
        source && published
          ? Array.from(
              { length: 8 },
              (_, index): SkillBundle => ({
                ...source,
                ref: `skill_catalog_${String(index)}`,
                currentRevision: {
                  ...published,
                  name: `Навык ${String(index + 1)}`,
                },
              }),
            ).filter((entry) =>
              entry.currentRevision?.name.includes(query.get("query") ?? ""),
            )
          : [];
      await route.fulfill({
        json: {
          items: entries,
          total: entries.length,
          nextPageToken: "",
        },
      });
      return;
    }
    if (
      path.startsWith(
        "/api/v1/skill-bundles/skill_synthetic/revisions/skill_revision/",
      ) &&
      skill?.draftRevision &&
      method === "POST"
    ) {
      expect(request.headers()["if-match"]).toBe(`"${String(skill.version)}"`);
      const input = request.postDataJSON() as
        | ContextRevisionDigestInput
        | SkillBundleReviewInput;
      expect(input.expectedDigest).toBe(skill.draftRevision.digest);
      const action = path.split("/").at(-1);
      if (action === "validation")
        skill = {
          ...skill,
          version: skill.version + 1,
          draftRevision: {
            ...skill.draftRevision,
            state: "VALIDATED",
            scanState: "CLEAN",
            scanEngine: "synthetic",
            scanDigest: "e".repeat(64),
            scannedAt: now,
          },
        };
      else if (action === "review") {
        expect(input).toEqual({
          expectedDigest: skill.draftRevision.digest,
          decision: "APPROVE",
          comment: "Проверено",
        });
        skill = {
          ...skill,
          version: skill.version + 1,
          draftRevision: {
            ...skill.draftRevision,
            state: "APPROVED",
            reviewedBy: "reviewer_synthetic",
            reviewedAt: now,
          },
        };
      } else if (action === "publication")
        skill = {
          ...skill,
          version: skill.version + 1,
          currentRevision: { ...skill.draftRevision, state: "PUBLISHED" },
          draftRevision: undefined,
        };
      else {
        await route.fallback();
        return;
      }
      events.push(`skill-${action}`);
      await route.fulfill({ json: skill });
      return;
    }
    await route.fallback();
  });
  await page.goto(`/projects/${projectRef}/context/memory/new`);
  await page.getByLabel("Название", { exact: true }).fill("Память synthetic");
  await page
    .getByRole("textbox", { name: "Содержание памяти", exact: true })
    .fill("Проверяемая заметка");
  await page.getByLabel("Хранить до", { exact: true }).fill("2026-10-01T12:00");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/context/memory/memory_synthetic$`));
  await page
    .getByRole("textbox", { name: "Содержание памяти", exact: true })
    .fill("Новая immutable заметка");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.locator(".context-provenance")).toContainText(
    "memory_revision_2",
  );
  await checkBinding("memory_synthetic", "memory_revision_2", "c".repeat(64));
  for (const name of [
    "Архивировать",
    "Восстановить",
    "Архивировать",
    "Удалить безвозвратно",
  ]) {
    await page.getByRole("button", { name, exact: true }).click();
    await page
      .getByRole("dialog", { name, exact: true })
      .getByRole("button", { name, exact: true })
      .click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }
  await expect(page.locator(".cm-content")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Сохранить", exact: true }),
  ).toBeDisabled();
  const expiry = await page.evaluate(() =>
    new Date(Date.now() + 4000).toISOString(),
  );
  const expiringMemory: KodexMemoryRecord = {
    ref: "memory_expiring",
    projectRef,
    version: 1,
    state: "ACTIVE",
    currentRevision: {
      ref: "memory_expiring_revision",
      revision: 1,
      title: "Истекающая память",
      summary: "Temporary synthetic memory",
      retentionUntil: expiry,
      digest: "c".repeat(64),
      provenance,
      redacted: false,
    },
    createdAt: now,
    updatedAt: now,
  };
  await page.goto(`/projects/${projectRef}/context/memory/memory_expiring`);
  await expect(
    page.getByRole("textbox", { name: "Содержание памяти", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Содержание памяти", exact: true }),
  ).toHaveCount(0, { timeout: 7000 });
  await expect(
    page.locator('.context-actions [data-state="EXPIRED"]'),
  ).toBeVisible();
  expect(expiryReads).toBe(2);
  await page.goto(`/projects/${projectRef}/context/skills/new`);
  await page.getByLabel("Название", { exact: true }).fill("Навык synthetic");
  await page
    .getByRole("button", { name: "Добавить файл", exact: true })
    .click();
  await page.getByRole("option", { name: /SKILL.md/ }).click();
  await expect(page.getByLabel("Путь файла", { exact: true })).toHaveValue(
    "SKILL.md",
  );
  await page
    .locator(".context-file")
    .getByRole("button", { name: "Удалить", exact: true })
    .click();
  await page.getByRole("button", { name: "Импорт Skill", exact: true }).click();
  const importer = page.getByRole("dialog", {
    name: "Импорт Skill",
    exact: true,
  });
  await importer.getByRole("button", { name: "SKILL.md", exact: true }).click();
  await importer
    .getByRole("textbox", { name: "SKILL.md", exact: true })
    .fill(
      "---\nname: Synthetic\ndescription: Synthetic skill\n---\nSynthetic instructions\n",
    );
  await importer
    .getByRole("button", { name: "Добавить SKILL.md", exact: true })
    .click();
  await importer
    .getByRole("button", { name: "Загрузить", exact: true })
    .click();
  await expect(importer.locator('[data-state="PENDING"]')).toBeVisible();
  await expect(
    importer.getByRole("button", { name: "Добавить в manifest", exact: true }),
  ).toBeDisabled();
  await importer
    .getByRole("button", { name: "Повторить", exact: true })
    .click();
  await expect(importer.locator('[data-state="CLEAN"]')).toBeVisible();
  await capture("skill-import");
  await importer
    .getByRole("button", { name: "Добавить в manifest", exact: true })
    .click();
  await expect(importer).toHaveCount(0);
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/context/skills/skill_synthetic$`));
  await page.getByRole("button", { name: "Проверить", exact: true }).click();
  await page.getByRole("button", { name: "Рассмотреть", exact: true }).click();
  const review = page.getByRole("dialog", { name: "Рассмотреть", exact: true });
  await review.getByLabel("Комментарий", { exact: true }).fill("Проверено");
  await review
    .getByRole("button", { name: "Рассмотреть", exact: true })
    .click();
  await page.getByRole("button", { name: "Опубликовать", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Опубликовать", exact: true }),
  ).toHaveCount(0);
  await checkBinding("skill_synthetic", "skill_revision", "d".repeat(64));
  expect(events).toEqual([
    "memory-create",
    "memory-revise",
    "memory-archive",
    "memory-restoration",
    "memory-archive",
    "memory-purge",
    "skill-create",
    "skill-validation",
    "skill-review",
    "skill-publication",
  ]);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
  await page.goto(`/projects/${projectRef}/files?view=skills`);
  await expect(page.locator(".context-row")).toHaveCount(8);
  const rows = page.locator(".context-rows");
  const bounds = await rows.boundingBox();
  const row = await page.locator(".context-row").first().boundingBox();
  expect(bounds).not.toBeNull();
  expect(row).not.toBeNull();
  if (!bounds || !row) throw new Error("Context catalog is not visible");
  expect(bounds.height).toBeLessThanOrEqual(row.height * 6 + 1);
  await page
    .getByRole("button", { name: "Развернуть список", exact: true })
    .click();
  const catalog = page.getByRole("dialog", { name: "Навыки", exact: true });
  await catalog.getByRole("searchbox").fill("Навык 8");
  await expect(catalog.locator(".context-row")).toHaveCount(1);
  await expect(catalog.locator(".context-row")).toContainText("Навык 8");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await catalog
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
}
