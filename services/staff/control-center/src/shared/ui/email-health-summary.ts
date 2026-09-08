const prefix = "email-health:v1:";
const reasons = new Set([
  "none",
  "auth_rejected",
  "credential_unavailable",
  "tls_unavailable",
  "network_unavailable",
  "response_invalid",
  "scan_limit",
  "configuration_invalid",
  "unavailable",
]);
const statuses = new Set(["ready", "not_ready", "not_configured"]);

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function stringRecord(value: unknown): value is Record<string, string> {
  return (
    record(value) &&
    Object.values(value).every((item) => typeof item === "string")
  );
}

// Только фиксированная HEALTH schema; provider text не становится ключом перевода.
export function localizeEmailHealth(
  value: string,
  localize: (key: string) => string,
): string | undefined {
  if (!value.startsWith(prefix)) return undefined;
  const invalid = () => localize("INTEGRATION_RESPONSE_INVALID");
  if (value.length > 1024) return invalid();
  try {
    const report: unknown = JSON.parse(value.slice(prefix.length));
    if (
      !record(report) ||
      Object.keys(report).sort().join() !== "protocols,status" ||
      typeof report.status !== "string" ||
      !["ready", "not_ready"].includes(report.status)
    )
      return invalid();
    const p = report.protocols;
    if (
      !stringRecord(p) ||
      Object.keys(p).sort().join() !==
        "imap,imap_reason,pop3,pop3_reason,smtp,smtp_reason"
    )
      return invalid();
    const rows = ["smtp", "imap", "pop3"] as const;
    for (const protocol of rows) {
      const status = p[protocol] ?? "",
        reason = p[`${protocol}_reason`] ?? "";
      if (
        !statuses.has(status) ||
        !reasons.has(reason) ||
        (status === "not_ready") === (reason === "none")
      )
        return invalid();
    }
    if (
      p.smtp === "not_configured" ||
      (p.imap === "not_configured" && p.pop3 === "not_configured")
    )
      return invalid();
    if (
      report.status === "ready" &&
      (p.smtp !== "ready" || (p.imap !== "ready" && p.pop3 !== "ready"))
    )
      return invalid();
    if (
      report.status === "not_ready" &&
      !rows.some((protocol) => p[protocol] === "not_ready")
    )
      return invalid();
    const canonical = JSON.stringify({
      status: report.status,
      protocols: {
        imap: p.imap,
        imap_reason: p.imap_reason,
        pop3: p.pop3,
        pop3_reason: p.pop3_reason,
        smtp: p.smtp,
        smtp_reason: p.smtp_reason,
      },
    });
    if (value !== prefix + canonical) return invalid();
    return rows
      .map(
        (protocol) =>
          `${protocol.toUpperCase()}: ${localize(`EMAIL_HEALTH_${((p[protocol] === "not_ready" ? p[`${protocol}_reason`] : p[protocol]) ?? "").toUpperCase()}`)}`,
      )
      .join("; ");
  } catch {
    return invalid();
  }
}
