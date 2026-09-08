import { test } from "@playwright/test";
import { persistSessionRenewalEvidence } from "../session-renewal-evidence";
import { observeFrame, socketProof } from "../session-renewal-proof";

// Без браузера, login и сети; fixture запускается только отдельным regression.
for (const status of ["PASS", "FAIL"] as const) {
  test(`safe evidence ${status}`, async ({ browserName }, testInfo) => {
    const proof = socketProof();
    observeFrame(
      proof,
      JSON.stringify({
        type: "SESSION_PROBLEM",
        title: "private-prompt-cookie-ticket-sentinel",
      }),
      "received",
    );
    const evidence = {
      schemaVersion: 1,
      browserName,
      requirement: "MVP-UI-11",
      status,
      tabs: [[proof]],
      windowSHA256: "a".repeat(64),
    };
    await persistSessionRenewalEvidence(testInfo, evidence);
    if (status === "FAIL") throw new Error("Expected fixture failure");
  });
}
