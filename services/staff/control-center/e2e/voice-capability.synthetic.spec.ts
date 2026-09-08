import { expect, test } from "@playwright/test";

// Этот файл намеренно не устанавливает synthetic recorder или microphone shim.
test("synthetic: native recorder capability и закрытый отказ без отправки", async ({
  page,
}, testInfo) => {
  await page.goto("http://127.0.0.1:43122/e2e/fixtures/voice.html");
  const capability = await page.evaluate(() => ({
    recorder: typeof MediaRecorder !== "undefined",
    secureContext: isSecureContext,
  }));
  testInfo.annotations.push({
    type: "native-recorder",
    description: capability.recorder
      ? "available; capture covered separately"
      : "unsupported; native capture/codec NOT RUN",
  });
  expect(capability.secureContext).toBe(true);
  if (capability.recorder) return;
  const field = page.getByTestId("textarea");
  await field.locator(".voice-input button").click();
  await expect(field.getByRole("alert")).toHaveText(
    "Микрофон или запись аудио недоступны в этом браузере.",
  );
  await expect(page.getByTestId("calls")).toHaveText("0");
  await expect(field.getByRole("textbox")).toHaveValue("Начало конец");
});
