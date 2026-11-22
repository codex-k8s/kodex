import type {
  EmailMailboxOperation,
  EmailMailboxProviderProfile,
  EmailMailboxSpecification,
} from "@/shared/api/generated/openapi/types.gen";

export const emailMailboxOperations: EmailMailboxOperation[] = [
  "HEALTH",
  "MAILBOXES",
  "LIST",
  "SEARCH",
  "FETCH",
  "DOWNLOAD",
  "SEND",
  "REPLY",
  "REPLY_ALL",
  "FORWARD",
  "DELETE",
  "RECEIPT",
  "THREAD",
  "ATTACHMENTS",
  "MARK_READ",
  "MARK_UNREAD",
  "MOVE",
  "ARCHIVE",
  "DRAFT_CREATE",
  "DRAFT_UPDATE",
  "DRAFT_DELETE",
];
const eachEffect = new Set<EmailMailboxOperation>([
  "SEND",
  "REPLY",
  "REPLY_ALL",
  "FORWARD",
  "DELETE",
]);

// Явный выбор меняет только draft endpoints. Адреса, recipients, folders,
// descriptor references и operation policies не выбираются за пользователя.
export function mailboxProviderProfile(
  value: EmailMailboxSpecification,
  profile: EmailMailboxProviderProfile,
): EmailMailboxSpecification {
  if (!["CUSTOM", "TIMEWEB_PASSWORD", "GMAIL_APP_PASSWORD"].includes(profile))
    throw new Error("Mailbox provider profile is invalid");
  // Меняем только собственные поля нового draft. Поверхностная копия сохраняет
  // остальные координаты и допускает reactive Vue props без DataCloneError.
  const next = { ...value };
  next.providerProfile = profile;
  if (profile === "CUSTOM") return next;
  const suffix = profile === "TIMEWEB_PASSWORD" ? "timeweb.ru" : "gmail.com";
  next.receiveProtocol = "IMAP";
  next.smtp = {
    ...next.smtp,
    host: `smtp.${suffix}`,
    serverName: `smtp.${suffix}`,
    port: 465,
    tlsMode: "IMPLICIT",
    authMethod: "PASSWORD",
  };
  next.imap = {
    ...next.imap,
    host: `imap.${suffix}`,
    serverName: `imap.${suffix}`,
    port: 993,
    tlsMode: "IMPLICIT",
    authMethod: "PASSWORD",
  };
  delete next.pop;
  return next;
}

// Это редактируемое предложение owner mailbox policy после явной кнопки.
// Оно не создаёт grant, scoped consent, Human Gate decision или отправку.
export function mailboxApprovalProfile(
  value: EmailMailboxSpecification,
): EmailMailboxSpecification {
  return {
    ...value,
    policies: emailMailboxOperations.map((operation) => ({
      operation,
      policy: eachEffect.has(operation) ? "HUMAN_GATE" : "ALLOW",
      folders: [...(value.allowedFolders ?? [])],
    })),
  };
}
