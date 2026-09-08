import { expect, it } from "vitest";
import { latestObservations, type UIObservation } from "./ui-evidence-ledger";
const entry: UIObservation = {
  browser: "chromium",
  fixtureManifestSHA256: "a".repeat(64),
  artifactSHA256: "b".repeat(64),
  versions: {
    harness: "1".repeat(40),
    api: "2".repeat(40),
    pwa: "3".repeat(40),
    servingManifestSHA256: "c".repeat(64),
  },
  variant: {
    id: "fixture-vfs-active",
    requirements: ["MVP-UI-25"],
    status: "FAIL",
    reason: "UI_ASSERTION_FAILED",
    locale: "ru",
    width: 1440,
    metrics: {},
    timestampUTC: "2026-09-08T10:00:00Z",
  },
};
it("new Firefox or different pinned fixture PASS never replaces Chromium FAIL", () => {
  const values: UIObservation[] = [
    entry,
    {
      ...entry,
      browser: "firefox",
      variant: {
        ...entry.variant,
        status: "PASS",
        timestampUTC: "2026-09-08T11:00:00Z",
      },
    },
    {
      ...entry,
      fixtureManifestSHA256: "d".repeat(64),
      variant: {
        ...entry.variant,
        status: "PASS",
        timestampUTC: "2026-09-08T12:00:00Z",
      },
    },
  ];
  expect(latestObservations(values).size).toBe(3);
  expect(
    [...latestObservations(values).values()].filter(
      (v) => v.variant.status === "FAIL",
    ),
  ).toHaveLength(1);
  expect(values[0]?.variant.status).toBe("FAIL");
});
it("only same exact browser/fixture/variant successor updates latest projection", () => {
  const next = {
    ...entry,
    variant: {
      ...entry.variant,
      status: "PASS" as const,
      timestampUTC: "2026-09-08T11:00:00Z",
    },
  };
  expect(latestObservations([next, entry]).size).toBe(1);
  expect(
    [...latestObservations([next, entry]).values()][0]?.variant.status,
  ).toBe("PASS");
});
