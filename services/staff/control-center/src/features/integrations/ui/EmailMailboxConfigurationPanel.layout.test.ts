import { readFileSync } from "node:fs";
import { expect, it } from "vitest";

const source = readFileSync(
  new URL("./EmailMailboxConfigurationPanel.vue", import.meta.url),
  "utf8",
);

it("обновляет pending-публикацию по realtime-сигналу без polling", () => {
  expect(source).toContain("props.realtimeRevision");
  expect(source).toContain('publication?.state !== "PENDING"');
  expect(source).not.toContain("deliveryTimer");
  expect(source).not.toContain("deliveryAttempts");
  expect(source).not.toContain("scheduleDelivery");
});
