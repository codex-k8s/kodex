import { expect, test, type Locator } from "@playwright/test";
import { prepareSyntheticMicrophone } from "./synthetic-microphone";

test.beforeEach(async ({ page, context, browserName }, testInfo) => {
  const mode =
    testInfo.project.metadata.voiceRecorder === "fixture"
      ? "fixture"
      : "native";
  testInfo.annotations.push({ type: "voice-recorder", description: mode });
  await prepareSyntheticMicrophone(page, context, browserName, undefined, mode);
});

async function dictate(section: Locator): Promise<void> {
  await section.locator('.voice-input[data-state="idle"] button').click();
  await expect(
    section.locator('.voice-input[data-state="recording"]'),
  ).toBeVisible();
  await new Promise((resolve) => setTimeout(resolve, 350));
  await section
    .locator('.voice-input[data-state="recording"] button')
    .first()
    .click();
  await expect(
    section.locator('.voice-input[data-state="idle"]'),
  ).toBeVisible();
}

for (const transition of [
  "route",
  "unmount",
  "availability",
  "pagehide",
] as const) {
  test(`synthetic: voice отменяет поздний ответ при ${transition}`, async ({
    page,
  }) => {
    await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
    await page.getByLabel("Задержка", { exact: true }).check();
    const field = page.getByTestId("textarea");
    await field.locator(".voice-input button").click();
    await expect(field.locator(".voice-input")).toHaveAttribute(
      "data-state",
      "recording",
    );
    await page.waitForTimeout(350);
    await field.locator(".voice-input button").first().click();
    await expect(field.locator(".voice-input")).toHaveAttribute(
      "data-state",
      "transcribing",
    );
    await expect(page.getByTestId("calls")).toHaveText("1");
    if (transition === "route")
      await page.getByRole("button", { name: "Перейти", exact: true }).click();
    else if (transition === "unmount")
      await page.getByLabel("Показывать поле", { exact: true }).uncheck();
    else if (transition === "availability")
      await page.getByLabel("Доступность", { exact: true }).uncheck();
    else
      await page.evaluate(() =>
        window.dispatchEvent(new PageTransitionEvent("pagehide")),
      );
    await page.waitForTimeout(1100);
    if (transition === "unmount")
      await page.getByLabel("Показывать поле", { exact: true }).check();
    if (transition === "availability")
      await page.getByLabel("Доступность", { exact: true }).check();
    await expect(field.getByRole("textbox")).toHaveValue("Начало конец");
    await expect(field.locator(".voice-input")).toHaveAttribute(
      "data-state",
      "idle",
    );
    await expect(page.getByTestId("calls")).toHaveText("1");
  });
}

for (const width of [390, 768, 1280, 1440, 1920, 2560, 2900]) {
  test(`synthetic: voice заменяет выделение и изолирует undo ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1080 });
    await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
    const textarea = page.getByTestId("textarea").getByRole("textbox");
    await textarea.focus();
    await textarea.evaluate((element) =>
      (element as HTMLTextAreaElement).setSelectionRange(7, 12),
    );
    await dictate(page.getByTestId("textarea"));
    await expect(textarea).toHaveValue("Начало диктовка ");
    await expect(textarea).toBeFocused();
    await textarea.press("Control+z");
    await expect(textarea).toHaveValue("Начало конец");
    const code = page.getByRole("textbox", { name: "Код", exact: true });
    await code.focus();
    await code.press("Control+End");
    await code.press("a");
    for (let index = 0; index < 6; index++) await code.press("Shift+ArrowLeft");
    await dictate(page.getByTestId("code"));
    await expect(code).toHaveText("Начало диктовка ");
    await expect(code).toBeFocused();
    await code.press("z");
    await code.press("Control+z");
    await expect(code).toHaveText("Начало диктовка ");
    await code.press("Control+z");
    await expect(code).toHaveText("Начало конецa");
    await code.press("Control+z");
    await expect(code).toHaveText("Начало конец");
  });
}

test("synthetic: voice сохраняет scroll длинного textarea", async ({
  page,
}) => {
  await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
  const field = page.getByTestId("textarea");
  const textarea = field.getByRole("textbox");
  await textarea.fill("Синтетическая строка\n".repeat(60));
  await textarea.evaluate((element) => {
    const input = element as HTMLTextAreaElement;
    input.focus();
    input.setSelectionRange(400, 400);
    input.scrollTop = 250;
  });
  const scrollTop = await textarea.evaluate((element) => element.scrollTop);
  await dictate(field);
  await expect(textarea).toBeFocused();
  expect(await textarea.evaluate((element) => element.scrollTop)).toBe(
    scrollTop,
  );
  await textarea.press("Control+z");
  await expect(textarea).toHaveValue("Синтетическая строка\n".repeat(60));
});

test("synthetic: unsupported codec показывает безопасную причину без audio", async ({
  page,
}) => {
  await page.addInitScript(() => {
    MediaRecorder.isTypeSupported = () => false;
  });
  await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
  const field = page.getByTestId("textarea");
  await field.locator(".voice-input button").click();
  await expect(field.getByRole("alert")).toContainText(
    "Формат записи не поддерживается",
  );
  await expect(page.getByTestId("calls")).toHaveText("0");
});

for (const width of [1440, 390]) {
  test(`synthetic: блокировка managed voice во время записи ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1080 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (["warning", "error"].includes(message.type()))
        errors.push(message.text());
    });
    page.on("requestfailed", (request) => errors.push(request.url()));
    await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
    await page
      .getByRole("button", { name: "Показать конфигурацию", exact: true })
      .click();
    const fields = page.getByTestId("managed-fields");
    const textareas = fields.locator("textarea");
    await expect(textareas).toHaveCount(4);
    await expect(fields.locator(".voice-input button")).toHaveCount(4);
    const original = await textareas.evaluateAll((items) =>
      items.map((item) => (item as HTMLTextAreaElement).value),
    );
    const firstVoice = fields.locator(".voice-input").first();
    await firstVoice.locator("button").click();
    await expect(firstVoice).toHaveAttribute("data-state", "recording");
    await fields.getByLabel("Блокировка конфигурации", { exact: true }).check();
    await expect(fields.locator(".voice-input button")).toHaveCount(0);
    await expect(fields.locator('[data-state="recording"]')).toHaveCount(0);
    for (const textarea of await textareas.all())
      await expect(textarea).toBeDisabled();
    await expect(page.getByTestId("calls")).toHaveText("0");
    expect(
      await textareas.evaluateAll((items) =>
        items.map((item) => (item as HTMLTextAreaElement).value),
      ),
    ).toEqual(original);
    await fields
      .getByLabel("Блокировка конфигурации", { exact: true })
      .uncheck();
    await expect(fields.locator(".voice-input button")).toHaveCount(4);
    const description = textareas.first();
    await description.focus();
    await description.evaluate((element) =>
      (element as HTMLTextAreaElement).setSelectionRange(7, 7),
    );
    await dictate(fields.locator(".voice-textarea").first());
    await expect(description).toHaveValue("Начало диктовка конец");
    await expect(page.getByTestId("calls")).toHaveText("1");
    await page.getByLabel("Доступность", { exact: true }).uncheck();
    await expect(fields.locator(".voice-input button")).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await fields.screenshot({
      path: testInfo.outputPath(`managed-voice-${String(width)}.png`),
    });
    expect(errors).toEqual([]);
  });
}

for (const width of [1440, 390]) {
  test(`synthetic: голосовой ввод, курсор и undo ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1080 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (["warning", "error"].includes(message.type()))
        errors.push(message.text());
    });
    await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
    const picker = page.getByTestId("picker");
    await picker
      .getByRole("button", { name: "Окружения", exact: true })
      .click();
    await page
      .getByRole("option", { name: "Первое окружение Ревизия 1", exact: true })
      .click();
    await page
      .getByRole("option", { name: "Второе окружение Ревизия 2", exact: true })
      .click();
    await expect(page.getByRole("listbox")).toHaveAttribute(
      "aria-multiselectable",
      "true",
    );
    await expect(
      page.getByRole("option", {
        name: "Закрытое окружение Нет разрешения",
        exact: true,
      }),
    ).toBeDisabled();
    await expect(picker.getByTestId("selection")).toHaveText(
      '["first","second"]',
    );
    await page.getByRole("combobox").fill("Первое");
    await expect(page.getByRole("option")).toHaveCount(1);
    await page.getByRole("combobox").press("Escape");
    await expect(
      picker.getByRole("button", { name: "Окружения", exact: true }),
    ).toBeFocused();
    await picker
      .getByRole("button", { name: "Очистить выбор", exact: true })
      .click();
    await expect(picker.getByTestId("selection")).toHaveText("[]");
    await page
      .getByRole("button", { name: "Показать список", exact: true })
      .click();
    const inline = page.getByTestId("inline-picker");
    await expect(inline.getByTestId("inline-calls")).toHaveText("0");
    await inline
      .getByRole("checkbox", { name: "Заблокировать список", exact: true })
      .uncheck();
    const first = inline.getByRole("option", {
      name: "Первое окружение Ревизия 1",
      exact: true,
    });
    const second = inline.getByRole("option", {
      name: "Второе окружение Ревизия 2",
      exact: true,
    });
    await first.click();
    await first.press("ArrowDown");
    await expect(second).toBeFocused();
    await second.press("Enter");
    await expect(inline.getByTestId("inline-selection")).toHaveText(
      '["first","second"]',
    );
    await second.press("ArrowDown");
    await expect(first).toBeFocused();
    await first.press("Space");
    await expect(inline.getByTestId("inline-selection")).toHaveText(
      '["second"]',
    );
    await inline
      .getByRole("checkbox", { name: "Заблокировать список", exact: true })
      .check();
    await expect(first).toBeDisabled();
    await inline
      .getByRole("checkbox", { name: "Заблокировать список", exact: true })
      .uncheck();
    await expect(inline.getByTestId("inline-calls")).toHaveText("2");
    await expect(first).toBeEnabled();
    await page
      .getByRole("button", { name: "Скрыть список", exact: true })
      .click();
    const textarea = page.getByRole("textbox", {
      name: "Обычный текст",
      exact: true,
    });
    await textarea.focus();
    await textarea.evaluate((element) => {
      (element as HTMLTextAreaElement).setSelectionRange(7, 7);
    });
    await dictate(page.getByTestId("textarea"));
    await expect(textarea).toHaveValue("Начало диктовка конец");
    await expect(textarea).toBeFocused();
    await textarea.press("Control+z");
    await expect(textarea).toHaveValue("Начало конец");

    const code = page.getByRole("textbox", { name: "Код", exact: true });
    await code.focus();
    await code.press("Control+Home");
    for (let position = 0; position < 7; position += 1)
      await code.press("ArrowRight");
    await dictate(page.getByTestId("code"));
    await expect(code).toHaveText("Начало диктовка конец");
    await expect(code).toBeFocused();
    await code.press("Control+z");
    await expect(code).toHaveText("Начало конец");
    await code.press("Control+Home");
    await code.press("Tab");
    await expect(code).toHaveText("  Начало конец");
    await code.press("Shift+Tab");
    await expect(code).toHaveText("Начало конец");
    await code.press("Control+m");
    await code.press("Tab");
    await expect(code).not.toBeFocused();
    await expect(page.getByTestId("diff")).toContainText("Сохранённый текст");
    await expect(page.getByTestId("diff")).toContainText("Начало конец");
    await expect(
      page.getByTestId("diff").locator('[contenteditable="true"]'),
    ).toHaveCount(0);
    await expect(
      page.getByTestId("diff").locator(".voice-input button"),
    ).toHaveCount(0);

    await expect(
      page.getByTestId("secret").locator(".voice-input button"),
    ).toHaveCount(0);
    await expect(
      page.getByTestId("readonly").locator(".voice-input button"),
    ).toHaveCount(0);
    await page.getByLabel("Блокировка", { exact: true }).check();
    await expect(
      page.getByTestId("fieldset").locator(".voice-input button"),
    ).toHaveCount(0);
    await page.getByLabel("Доступность", { exact: true }).uncheck();
    await expect(page.locator(".voice-input button")).toHaveCount(0);
    await expect
      .poll(() =>
        textarea.evaluate((element) => getComputedStyle(element).paddingBottom),
      )
      .not.toBe("50px");
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`voice-${String(width)}.png`),
      fullPage: true,
    });
    expect(errors).toEqual([]);
  });
}

for (const width of [390, 2900]) {
  test(`synthetic: typed STT rate limit без автоматического повтора ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1600 });
    await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
    await page.getByLabel("Ограничение частоты", { exact: true }).check();
    const field = page.getByTestId("textarea");
    await field.locator(".voice-input button").click();
    await expect(
      field.locator('.voice-input[data-state="recording"]'),
    ).toBeVisible();
    await page.waitForTimeout(350);
    await field.locator(".voice-input button").first().click();
    await expect(field.getByRole("alert")).toContainText(
      "Подождите минимум 30 с",
    );
    await expect(field.getByRole("textbox")).toHaveValue("Начало конец");
    await expect(page.getByTestId("calls")).toHaveText("1");
    await page.waitForTimeout(1100);
    await expect(page.getByTestId("calls")).toHaveText("1");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await field.screenshot({
      path: testInfo.outputPath(`stt-rate-limit-${String(width)}.png`),
    });
  });
}

test("synthetic: отмена, ошибка и единственная активная запись", async ({
  page,
}) => {
  await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
  const text = page.getByTestId("textarea");
  const code = page.getByTestId("code");
  await text.locator(".voice-input button").click();
  await expect(
    text.locator('.voice-input[data-state="recording"]'),
  ).toBeVisible();
  await code.locator(".voice-input button").click();
  await expect(
    code.locator('.voice-input[data-state="recording"]'),
  ).toBeVisible();
  await expect(text.locator('.voice-input[data-state="idle"]')).toBeVisible();
  await code.getByRole("button", { name: "Отмена", exact: true }).click();
  await expect(page.getByTestId("calls")).toHaveText("0");

  await page.getByLabel("Задержка", { exact: true }).check();
  await text.locator(".voice-input button").click();
  await expect(
    text.locator('.voice-input[data-state="recording"]'),
  ).toBeVisible();
  await page.waitForTimeout(350);
  await text.locator(".voice-input button").first().click();
  await expect(
    text.locator('.voice-input[data-state="transcribing"]'),
  ).toBeVisible();
  await text.getByRole("button", { name: "Отмена", exact: true }).click();
  await expect(text.getByRole("textbox")).toHaveValue("Начало конец");
  await page.getByLabel("Задержка", { exact: true }).uncheck();
  await page.getByLabel("Ошибка", { exact: true }).check();
  await text.locator(".voice-input button").click();
  await expect(
    text.locator('.voice-input[data-state="recording"]'),
  ).toBeVisible();
  await page.waitForTimeout(350);
  await text.locator(".voice-input button").first().click();
  await expect(text.getByRole("alert")).toBeVisible();
  await expect(text.getByRole("textbox")).toHaveValue("Начало конец");
  await expect(page.getByTestId("calls")).toHaveText("2");
  await page.waitForTimeout(1100);
  await expect(page.getByTestId("calls")).toHaveText("2");
});
