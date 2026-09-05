import { expect, test, type Locator } from "@playwright/test";

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

for (const width of [1440, 390]) {
  test(`synthetic: голосовой ввод, курсор и undo ${String(width)}px`, async ({
    page,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1080 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (["warning", "error"].includes(message.type()))
        errors.push(message.text());
    });
    await context.grantPermissions(["microphone"], {
      origin: "http://127.0.0.1:43122",
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

test("synthetic: отмена, ошибка и единственная активная запись", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["microphone"], {
    origin: "http://127.0.0.1:43122",
  });
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
