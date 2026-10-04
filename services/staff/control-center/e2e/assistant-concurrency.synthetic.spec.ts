import { expect, test, type Page } from "@playwright/test";
import type { AssistantConversation } from "../src/shared/api/generated/openapi/types.gen";
import {
  AssistantConcurrencyNetwork,
  conversationRef,
  projectA,
  projectB,
} from "./fixtures/assistant-concurrency.network";

interface Diagnostic {
  initialized: boolean;
  projectRef: string;
  state: string;
  sequence: number;
  selectedRef?: string;
  conversations: AssistantConversation[];
}
function collectErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (["warning", "error"].includes(message.type()))
      errors.push(message.text());
  });
  return errors;
}
function translated(page: Page, ru: string, en: string): string {
  return new URL(page.url()).searchParams.get("locale") === "en" ? en : ru;
}
async function state(page: Page): Promise<Diagnostic> {
  return JSON.parse(
    (await page.getByTestId("concurrency-state").textContent()) ?? "{}",
  ) as Diagnostic;
}
function entry(page: Page, ref: string) {
  return page.locator(
    `.assistant-conversation-sidebar .assistant-conversation-entry[data-conversation-ref="${ref}"] .assistant-conversation-entry__select`,
  );
}
async function open(page: Page): Promise<void> {
  await expect.poll(async () => (await state(page)).state).toBe("live");
  await page
    .getByRole("button", {
      name: translated(page, "Открыть Kodex", "Open Kodex"),
      exact: true,
    })
    .click();
  await expect(page.locator("#assistant-workspace")).toBeVisible();
}
async function setup(
  page: Page,
  locale: "ru" | "en",
  width = 1440,
): Promise<AssistantConcurrencyNetwork> {
  await page.setViewportSize({ width, height: 1000 });
  const network = new AssistantConcurrencyNetwork();
  await network.install(page);
  await page.goto(`/e2e/fixtures/assistant-concurrency.html?locale=${locale}`);
  await open(page);
  return network;
}

for (const locale of ["ru", "en"] as const) {
  test(`2 → 10 активных, 11-й в очереди: draft/queue/interrupt/Stop/delete изолированы ${locale}`, async ({
    page,
  }, testInfo) => {
    const errors = collectErrors(page);
    const network = await setup(page, locale);
    const first = conversationRef(projectA, 1);
    const second = conversationRef(projectA, 2);
    const eleventh = conversationRef(projectA, 11);
    const dialog = page.locator("#assistant-workspace");
    const composer = dialog.locator(".assistant-composer textarea");
    await expect
      .poll(
        async () =>
          (await state(page)).conversations
            .flatMap((value) => value.turns)
            .filter((turn) => turn.state === "RUNNING").length,
      )
      .toBe(2);
    await entry(page, first).click();
    await composer.fill("Черновик первого диалога");
    await entry(page, second).click();
    await expect(composer).toHaveValue("");
    await composer.fill("Черновик второго диалога");
    await entry(page, first).click();
    await expect(composer).toHaveValue("Черновик первого диалога");
    await expect(dialog.locator(".assistant-chat-log")).toContainText(
      `История ${first}`,
    );
    await expect(dialog.locator(".assistant-chat-log")).not.toContainText(
      `История ${second}`,
    );
    await composer.focus();
    network.admitTen();
    await expect
      .poll(
        async () =>
          (await state(page)).conversations
            .flatMap((value) => value.turns)
            .filter((turn) => turn.state === "RUNNING").length,
      )
      .toBe(10);
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue("Черновик первого диалога");
    const neighbors = structuredClone(
      network.conversations(projectA).filter((value) => value.ref !== first),
    );
    const reads = network.reads;
    await dialog
      .getByRole("button", {
        name: translated(page, "Добавить сообщение в очередь", "Queue message"),
        exact: true,
      })
      .click();
    await expect.poll(() => network.mutations.length).toBe(1);
    expect(network.mutations[0]).toMatchObject({
      conversationRef: first,
      mode: "QUEUE",
      content: "Черновик первого диалога",
    });
    await expect(composer).toHaveValue("");
    await expect
      .poll(async () =>
        (await state(page)).conversations.filter(
          (value) => value.ref !== first,
        ),
      )
      .toEqual(neighbors);
    await composer.fill("Срочное сообщение первого диалога");
    await dialog
      .getByRole("button", {
        name: translated(
          page,
          "Остановить текущий ход и отправить сейчас",
          "Stop the current turn and send now",
        ),
        exact: true,
      })
      .click();
    await expect.poll(() => network.mutations.length).toBe(2);
    expect(network.mutations[1]).toMatchObject({
      conversationRef: first,
      mode: "INTERRUPT_ACTIVE",
    });
    await expect
      .poll(async () =>
        (await state(page)).conversations
          .find((value) => value.ref === first)
          ?.turns.map((turn) => turn.state),
      )
      .toEqual(["CANCELLED", "QUEUED", "RUNNING"]);
    await expect
      .poll(async () =>
        (await state(page)).conversations.filter(
          (value) => value.ref !== first,
        ),
      )
      .toEqual(neighbors);
    await dialog
      .getByRole("button", {
        name: translated(
          page,
          "Остановить текущий ход",
          "Stop the current turn",
        ),
        exact: true,
      })
      .click();
    await expect.poll(() => network.mutations.length).toBe(3);
    await expect
      .poll(async () =>
        (await state(page)).conversations
          .find((value) => value.ref === first)
          ?.turns.map((turn) => turn.state),
      )
      .toEqual(["CANCELLED", "QUEUED", "CANCELLED"]);
    await expect
      .poll(async () =>
        (await state(page)).conversations.filter(
          (value) => value.ref !== first,
        ),
      )
      .toEqual(neighbors);
    expect(
      network.conversations(projectA).filter((value) => value.ref !== first),
    ).toEqual(neighbors);
    expect(network.values.get(eleventh)?.turns[0]?.state).toBe("QUEUED");

    // Realtime readback нового retry, не UI-команда retry и не controller restart.
    network.retryReadback(first);
    await expect(dialog.locator(".assistant-chat-log")).toContainText(
      `Повторный ход ${first}`,
    );
    expect(
      (await state(page)).conversations
        .find((value) => value.ref === first)
        ?.turns.at(-1)?.runRef,
    ).toBe(`run_${first}_retry_1`);
    await expect
      .poll(async () =>
        (await state(page)).conversations.filter(
          (value) => value.ref !== first,
        ),
      )
      .toEqual(neighbors);
    expect(
      network.conversations(projectA).filter((value) => value.ref !== first),
    ).toEqual(neighbors);
    await dialog
      .locator(".assistant-conversation-title")
      .getByRole("button", {
        name: translated(page, "Удалить диалог", "Delete conversation"),
        exact: true,
      })
      .click();
    await page.locator("[data-confirm-dialog-primary]").click();
    await expect.poll(() => network.mutations.length).toBe(4);
    expect(network.mutations.at(-1)?.conversationRef).toBe(first);
    await expect(entry(page, first)).toHaveCount(0);
    await expect
      .poll(async () => (await state(page)).conversations)
      .toEqual(neighbors);
    expect(network.values.get(first)?.state).toBe("ARCHIVED");
    expect(
      network.conversations(projectA).filter((value) => value.ref !== first),
    ).toEqual(neighbors);
    await entry(page, second).click();
    await expect(composer).toHaveValue("Черновик второго диалога");
    expect(network.reads).toBe(reads);
    expect(network.unexpected).toEqual([]);
    expect(errors).toEqual([]);
    await page.screenshot({
      path: testInfo.outputPath("ten-conversations-isolation.png"),
      fullPage: true,
    });
  });

  test(`Смена проекта и WS rejoin после synthetic server restart не перепривязывают историю ${locale}`, async ({
    page,
  }, testInfo) => {
    const errors = collectErrors(page);
    const network = await setup(page, locale);
    const first = conversationRef(projectA, 1);
    const other = conversationRef(projectB, 1);
    const dialog = page.locator("#assistant-workspace");
    const composer = dialog.locator(".assistant-composer textarea");
    await entry(page, first).click();
    await composer.fill("Черновик проекта A");
    await dialog
      .locator(".assistant-drawer__header")
      .getByRole("button", {
        name: translated(page, "Закрыть", "Close"),
        exact: true,
      })
      .click();
    await page.locator("[data-confirm-dialog-primary]").click();
    await expect(dialog).toBeHidden();
    await page.getByTestId("project-b").click();
    await expect
      .poll(async () => (await state(page)).projectRef)
      .toBe(projectB);
    await expect.poll(() => network.resumes.at(-1)?.projectRef).toBe(projectB);
    await open(page);
    await entry(page, other).click();
    await expect(composer).toHaveValue("");
    await expect(dialog.locator(".assistant-chat-log")).toContainText(
      `История ${other}`,
    );
    await expect(entry(page, first)).toHaveCount(0);
    const beforeForeign = await state(page);
    const beforeForeignResumes = network.resumes.length;
    network.sendForeignProjectSnapshot();
    // Чужой project frame закрыто отклоняется: новый resume того же project.
    await expect
      .poll(() => network.resumes.length)
      .toBe(beforeForeignResumes + 1);
    await expect.poll(async () => (await state(page)).state).toBe("live");
    await expect(composer).toHaveValue("");
    expect((await state(page)).conversations).toEqual(
      beforeForeign.conversations,
    );
    await dialog
      .locator(".assistant-drawer__header")
      .getByRole("button", {
        name: translated(page, "Закрыть", "Close"),
        exact: true,
      })
      .click();
    await page.getByTestId("project-a").click();
    await expect.poll(() => network.resumes.at(-1)?.projectRef).toBe(projectA);
    await open(page);
    await entry(page, first).click();
    await expect(composer).toHaveValue("Черновик проекта A");
    const beforeRestart = structuredClone((await state(page)).conversations);
    const beforeRestartSequence = (await state(page)).sequence;
    const reads = network.reads;
    const resumes = network.resumes.length;
    const oldRequest = network.resumes.at(-1)?.requestRef;
    await network.restart();
    await expect.poll(() => network.resumes.length).toBe(resumes + 1);
    await expect.poll(async () => (await state(page)).state).toBe("live");
    expect(network.resumes.at(-1)?.requestRef).not.toBe(oldRequest);
    expect(network.resumes.at(-1)?.projectRef).toBe(projectA);
    expect(network.resumes.at(-1)?.platformAfterSequence).toBe(
      beforeRestartSequence,
    );
    expect((await state(page)).conversations).toEqual(beforeRestart);
    await expect(dialog).toHaveAttribute("data-conversation-ref", first);
    await expect(composer).toHaveValue("Черновик проекта A");
    if (!oldRequest) throw new Error("Synthetic old request is missing");
    network.sendStaleRequestSnapshot(oldRequest);
    network.publish();
    await expect
      .poll(async () => (await state(page)).sequence)
      .toBe(network.sequence);
    expect((await state(page)).conversations).toEqual(beforeRestart);
    expect(network.reads).toBe(reads);
    expect(network.mutations).toEqual([]);
    expect(network.unexpected).toEqual([]);
    expect(errors).toEqual([]);
    await page.screenshot({
      path: testInfo.outputPath("project-rejoin-isolation.png"),
      fullPage: true,
    });
  });

  test(`Мобильная история 11 диалогов сохраняет черновики при 10 активных ходах ${locale}`, async ({
    page,
  }, testInfo) => {
    const errors = collectErrors(page);
    const network = await setup(page, locale, 390);
    const dialog = page.locator("#assistant-workspace");
    const composer = dialog.locator(".assistant-composer textarea");
    const first = conversationRef(projectA, 1);
    const eleventh = conversationRef(projectA, 11);
    async function select(ref: string): Promise<void> {
      await dialog
        .getByRole("button", {
          name: translated(page, "История диалогов", "Conversation history"),
          exact: true,
        })
        .click();
      const history = dialog.locator(".assistant-history__menu");
      await expect(
        history.locator("button[data-conversation-ref]"),
      ).toHaveCount(11);
      await history.locator(`button[data-conversation-ref="${ref}"]`).click();
      await expect(dialog).toHaveAttribute("data-conversation-ref", ref);
    }
    await select(first);
    await composer.fill("Мобильный черновик первого диалога");
    network.admitTen();
    await expect
      .poll(
        async () =>
          (await state(page)).conversations
            .flatMap((value) => value.turns)
            .filter((turn) => turn.state === "RUNNING").length,
      )
      .toBe(10);
    await select(eleventh);
    await expect(composer).toHaveValue("");
    await expect(dialog.locator(".assistant-chat-log")).toContainText(
      `История ${eleventh}`,
    );
    expect(
      (await state(page)).conversations.find((value) => value.ref === eleventh)
        ?.turns[0]?.state,
    ).toBe("QUEUED");
    await composer.fill("Мобильный черновик очереди");
    await select(first);
    await expect(composer).toHaveValue("Мобильный черновик первого диалога");
    expect(
      await dialog.evaluate(
        (element) => element.scrollWidth <= element.clientWidth + 1,
      ),
    ).toBe(true);
    expect(network.mutations).toEqual([]);
    expect(network.unexpected).toEqual([]);
    expect(errors).toEqual([]);
    await page.screenshot({
      path: testInfo.outputPath("mobile-eleven-dialogs.png"),
      fullPage: true,
    });
  });
}
