import { describe, expect, it } from "vitest";
import { memoryContentAvailable } from "./retention";
describe("Memory retention", () => {
  it("закрывает содержимое точно на границе, при redaction и неизвестном сроке", () => {
    const revision = {
      redacted: false,
      retentionUntil: "2026-09-05T01:00:00Z",
    };
    const expiry = Date.parse(revision.retentionUntil);
    expect(memoryContentAvailable(revision, expiry - 1)).toBe(true);
    expect(memoryContentAvailable(revision, expiry)).toBe(false);
    expect(
      memoryContentAvailable({ ...revision, redacted: true }, expiry - 1),
    ).toBe(false);
    expect(
      memoryContentAvailable(
        { ...revision, retentionUntil: "invalid" },
        expiry - 1,
      ),
    ).toBe(false);
  });
});
