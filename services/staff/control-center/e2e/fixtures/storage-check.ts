import type { Page } from "@playwright/test";

// browserStorageContains проверяет обе browser storage на значение без записи или вывода.
// Получение значения и решение о результате остаются у вызывающего сценария.
export async function browserStorageContains(
  page: Page,
  value: string,
): Promise<boolean> {
  return await page.evaluate((value) => {
    for (const storage of [window.localStorage, window.sessionStorage]) {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        if (key && storage.getItem(key)?.includes(value)) return true;
      }
    }
    return false;
  }, value);
}
