import { describe, expect, it } from "vitest";
import { localizeEmailHealth } from "./email-health-summary";
const report =
  'email-health:v1:{"status":"not_ready","protocols":{"imap":"not_configured","imap_reason":"none","pop3":"not_ready","pop3_reason":"auth_rejected","smtp":"ready","smtp_reason":"none"}}';
describe("Закрытая почтовая диагностика", () => {
  it("сохраняет причины по протоколам и старые outcomes", () => {
    expect(localizeEmailHealth(report, (key) => key)).toBe(
      "SMTP: EMAIL_HEALTH_READY; IMAP: EMAIL_HEALTH_NOT_CONFIGURED; POP3: EMAIL_HEALTH_AUTH_REJECTED",
    );
    expect(
      localizeEmailHealth("i18n:INTEGRATION_UNAVAILABLE", (key) => key),
    ).toBeUndefined();
    expect(
      localizeEmailHealth("Already localized", (key) => key),
    ).toBeUndefined();
  });
  it.each([
    report + " ",
    report.replace("auth_rejected", "fixture-private-text"),
    report.replace('"smtp_reason":"none"', '"smtp_reason":"auth_rejected"'),
    report.replace('"status":', '"host":"example.invalid","status":'),
  ])("отклоняет чужие поля и причины", (value) => {
    expect(localizeEmailHealth(value, (key) => key)).toBe(
      "INTEGRATION_RESPONSE_INVALID",
    );
  });
});
