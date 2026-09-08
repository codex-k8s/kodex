import { createHash } from "node:crypto";
import { open } from "node:fs/promises";
import type { TestInfo } from "@playwright/test";

const evidenceName = "session-renewal-safe-evidence";

async function createExclusive(path: string, content: string): Promise<void> {
  const file = await open(path, "wx", 0o600);
  try {
    await file.writeFile(content, "utf8");
    await file.sync();
  } finally {
    await file.close();
  }
}

// Вызывается только с безопасной projection, сформированной наблюдателем.
// Файл принадлежит запуску, а не reporter; существующий результат не заменяется.
export async function persistSessionRenewalEvidence(
  testInfo: Pick<TestInfo, "outputPath" | "attach">,
  evidence: Record<string, unknown>,
): Promise<void> {
  const payload = `${JSON.stringify(evidence, null, 2)}\n`;
  if (Buffer.byteLength(payload) > 65_536) {
    throw new Error("Session evidence exceeds its size limit");
  }
  const path = testInfo.outputPath(`${evidenceName}.json`);
  await createExclusive(path, payload);
  const digest = createHash("sha256").update(payload).digest("hex");
  await createExclusive(
    testInfo.outputPath(`${evidenceName}.sha256`),
    `${digest}\n`,
  );
  await testInfo.attach(evidenceName, {
    path,
    contentType: "application/json",
  });
}
