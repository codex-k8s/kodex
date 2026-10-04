import { expect, test, type Page } from "@playwright/test";
import type {
  AssistantConversation,
  Run,
} from "../src/shared/api/generated/openapi/types.gen";
import {
  AssistantRetryNetwork,
  failedRunRef,
  firstConversationRef,
  retriedRunRef,
} from "./fixtures/assistant-retry.network";

interface Diagnostic {
  initialized: boolean;
  route: string;
  state: string;
  runState: Record<string, { state: string }>;
  runLoading: boolean;
  selectedRef?: string;
  conversations: AssistantConversation[];
  runs: Record<string, Run>;
  artifacts: Record<string, unknown>;
}
async function diagnostic(page: Page): Promise<Diagnostic> {
  return JSON.parse(
    (await page.getByTestId("retry-state").textContent()) ?? "{}",
  ) as Diagnostic;
}
async function openAssistant(page: Page, locale: "ru" | "en"): Promise<void> {
  await page
    .getByRole("button", {
      name: locale === "ru" ? "Открыть Kodex" : "Open Kodex",
      exact: true,
    })
    .click();
  await expect(page.locator("#assistant-workspace")).toBeVisible();
  if ((await diagnostic(page)).selectedRef !== firstConversationRef) {
    const entry = page.locator(
      `.assistant-conversation-entry[data-conversation-ref="${firstConversationRef}"] .assistant-conversation-entry__select`,
    );
    if (!(await entry.isVisible()))
      await page
        .getByRole("button", {
          name: locale === "ru" ? "История диалогов" : "Conversation history",
          exact: true,
        })
        .click();
    await entry.click();
  }
}
for (const scope of ["SYSTEM", "PROJECT"] as const)
  for (const [width, locale] of [
    [1440, "ru"],
    [390, "en"],
  ] as const)
    test(`retry exact ${scope} ${String(width)} ${locale}: OCC→fresh read→lost ACK→same-chat/rejoin`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 1000 });
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      page.on("console", (message) => {
        // Единственные ожидаемые ошибки transport — проверяемый OCC и потеря ACK.
        if (
          ["warning", "error"].includes(message.type()) &&
          !/status of (412|503)/.test(message.text())
        )
          errors.push(message.text());
      });
      const network = new AssistantRetryNetwork(scope);
      await network.install(page);
      await page.goto(
        `/e2e/fixtures/assistant-retry.html?scope=${scope}&locale=${locale}`,
      );
      await expect
        .poll(async () => (await diagnostic(page)).state)
        .toBe("live");
      await openAssistant(page, locale);
      const before = await diagnostic(page);
      const neighbors = before.conversations.filter(
        (value) => value.ref !== firstConversationRef,
      );
      expect(neighbors).toHaveLength(9);
      const first = before.conversations.find(
        (value) => value.ref === firstConversationRef,
      );
      expect(first?.assistantScope).toBe(scope);
      await page.screenshot({
        path: testInfo.outputPath(
          `assistant-turn-link-${scope}-${String(width)}-${locale}.png`,
        ),
        fullPage: true,
      });
      const openRun = page.locator(
        '.assistant-message[data-turn-sequence="1"] .assistant-message__run-link',
      );
      await expect(openRun).toHaveAttribute(
        "href",
        scope === "PROJECT"
          ? `/projects/prj_retry_fixture/runs/${failedRunRef}`
          : `/runs/${failedRunRef}`,
      );
      await openRun.click();
      await expect
        .poll(async () => (await diagnostic(page)).route)
        .toContain(failedRunRef);
      const retry = page.getByRole("button", {
        name: locale === "ru" ? "Повторить попытку" : "Retry attempt",
        exact: true,
      });
      await expect(retry).toBeVisible();
      await expect
        .poll(async () => Object.keys((await diagnostic(page)).artifacts))
        .toContain("art_retry_child");
      await expect
        .poll(
          async () => (await diagnostic(page)).runState[failedRunRef]?.state,
        )
        .toBe("live");
      await expect
        .poll(async () => (await diagnostic(page)).runLoading)
        .toBe(false);
      const beforeConflict = structuredClone([
        ...network.conversations.values(),
      ]);
      network.staleVersion();
      await retry.click();
      await expect.poll(() => network.commands.length).toBe(3);
      await expect(retry).toBeEnabled();
      expect(network.commands[0]).toMatchObject({
        runRef: failedRunRef,
        version: '"1"',
        status: 412,
      });
      expect(network.commands.map((value) => value.status)).toEqual([
        412, 412, 412,
      ]);
      expect(
        new Set(network.commands.map((value) => value.idempotencyKey)).size,
      ).toBe(1);
      expect([...network.conversations.values()]).toEqual(beforeConflict);
      expect(network.runs.has(retriedRunRef)).toBe(false);
      await page.reload();
      await expect
        .poll(async () => (await diagnostic(page)).state)
        .toBe("live");
      await openAssistant(page, locale);
      await page
        .locator(
          '.assistant-message[data-turn-sequence="1"] .assistant-message__run-link',
        )
        .click();
      await expect(retry).toBeVisible();
      network.loseAcknowledgement = true;
      await retry.click();
      await expect
        .poll(async () => (await diagnostic(page)).route)
        .toContain(retriedRunRef);
      expect(network.commands.map((value) => value.status)).toEqual([
        412, 412, 412, 503, 200,
      ]);
      expect(network.commands[3]?.version).toBe('"2"');
      expect(network.commands[4]?.version).toBe('"2"');
      expect(network.commands[3]?.idempotencyKey).toBe(
        network.commands[4]?.idempotencyKey,
      );
      expect(network.commands[0]?.idempotencyKey).not.toBe(
        network.commands[3]?.idempotencyKey,
      );
      const afterRun = (await diagnostic(page)).runs[retriedRunRef];
      expect(afterRun).toMatchObject({
        ref: retriedRunRef,
        sessionRef: "ses_retry_fixture",
        attempt: 2,
        retryOfRunRef: failedRunRef,
      });
      await expect(page.locator('[data-edge-type="RETRY_OF"]')).toHaveCount(1);
      await expect
        .poll(
          async () =>
            (await diagnostic(page)).conversations
              .find((value) => value.ref === firstConversationRef)
              ?.turns.at(-1)?.runRef,
        )
        .toBe(retriedRunRef);
      const after = await diagnostic(page);
      expect(
        after.conversations.filter(
          (value) => value.ref !== firstConversationRef,
        ),
      ).toEqual(neighbors);
      const retriedConversation = after.conversations.find(
        (value) => value.ref === firstConversationRef,
      );
      expect(retriedConversation?.assistantRef).toBe(first?.assistantRef);
      expect(retriedConversation?.assistantProfileRef).toBe(
        first?.assistantProfileRef,
      );
      expect(retriedConversation?.projectRef).toBe(first?.projectRef);
      expect(retriedConversation?.turns).toHaveLength(2);
      const resumes = network.resumes.length;
      await network.restart();
      await expect.poll(() => network.resumes.length).toBeGreaterThan(resumes);
      await expect
        .poll(async () => (await diagnostic(page)).state)
        .toBe("live");
      await expect
        .poll(
          async () => (await diagnostic(page)).runs[retriedRunRef]?.sessionRef,
        )
        .toBe("ses_retry_fixture");
      const rejoined = await diagnostic(page);
      await expect
        .poll(async () => Object.keys((await diagnostic(page)).artifacts))
        .toContain("art_retry_child");
      await expect(page.locator("#run-graph-panel")).toBeVisible();
      await expect(page.locator(".run-node__kind")).toHaveText([
        locale === "ru" ? "Помощник Kodex" : "Kodex assistant",
        locale === "ru" ? "Помощник Kodex" : "Kodex assistant",
      ]);
      await expect(page.locator(".graph-legend__states")).toContainText(
        locale === "ru" ? "Помощник Kodex" : "Kodex assistant",
      );
      await expect(page.locator(".run-node__role").first()).toHaveAttribute(
        "title",
        locale === "ru" ? "Помощник Kodex" : "Kodex assistant",
      );
      expect(
        rejoined.conversations.filter(
          (value) => value.ref !== firstConversationRef,
        ),
      ).toEqual(neighbors);
      expect(rejoined.runs[retriedRunRef]?.sessionRef).toBe(
        "ses_retry_fixture",
      );
      expect(network.unexpected).toEqual([]);
      expect(network.reads).toContain(retriedRunRef);
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      );
      expect(overflow).toBe(false);
      await page
        .getByRole("button", {
          name: locale === "ru" ? "Вместить" : "Fit",
          exact: true,
        })
        .click();
      await expect
        .poll(() =>
          page.locator(".run-node").evaluateAll((nodes) => {
            const canvas = document
              .querySelector("#run-graph-panel")
              ?.getBoundingClientRect();
            return (
              !!canvas &&
              nodes.length === 2 &&
              nodes.every((node) => {
                const bounds = node.getBoundingClientRect();
                return (
                  bounds.left >= canvas.left &&
                  bounds.right <= canvas.right &&
                  bounds.top >= canvas.top &&
                  bounds.bottom <= canvas.bottom
                );
              })
            );
          }),
        )
        .toBe(true);
      await page.screenshot({
        path: testInfo.outputPath(
          `assistant-retry-${scope}-${String(width)}-${locale}.png`,
        ),
        fullPage: true,
      });
      expect(errors).toEqual([]);
    });
