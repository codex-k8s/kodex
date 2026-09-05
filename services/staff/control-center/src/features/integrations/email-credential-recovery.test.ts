import { expect, it } from "vitest";
import {
  mailboxCredentialRecoveryKey,
  pendingMailboxCredential,
  rememberMailboxCredential,
  forgetMailboxCredential,
  type MailboxCredentialAttempt,
} from "./email-credential-recovery";
function storage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
}
const attempt: MailboxCredentialAttempt = {
  connectionRef: "connection_one",
  connectionVersion: 3,
  kind: "AUTH_SECRET",
  key: "12345678-1234-1234-1234-123456789abc",
};
it("сохраняет ровно безопасные metadata и восстанавливает их после закрытия формы", () => {
  const cache = storage();
  rememberMailboxCredential(
    {
      ...attempt,
      value: "private",
      inputDigest: "private",
    } as MailboxCredentialAttempt,
    cache,
  );
  expect(pendingMailboxCredential(attempt.connectionRef, cache)).toEqual(
    attempt,
  );
  expect(cache.getItem(mailboxCredentialRecoveryKey)).not.toContain("private");
  expect(cache.getItem(mailboxCredentialRecoveryKey)).not.toContain("Digest");
  forgetMailboxCredential(attempt, cache);
  expect(
    pendingMailboxCredential(attempt.connectionRef, cache),
  ).toBeUndefined();
});
it("не заменяет неопределённый ключ новой попыткой и не удаляет иной receipt", () => {
  const cache = storage();
  rememberMailboxCredential(attempt, cache);
  expect(() =>
    rememberMailboxCredential({ ...attempt, connectionVersion: 4 }, cache),
  ).toThrow();
  forgetMailboxCredential({ ...attempt, key: "other" }, cache);
  expect(pendingMailboxCredential(attempt.connectionRef, cache)).toEqual(
    attempt,
  );
});
it("отклоняет повреждённые либо расширенные сохранённые данные", () => {
  const cache = storage();
  for (const item of [
    { ...attempt, value: "private" },
    { ...attempt, connectionVersion: 0 },
  ]) {
    cache.setItem(mailboxCredentialRecoveryKey, JSON.stringify([item]));
    expect(() =>
      pendingMailboxCredential(attempt.connectionRef, cache),
    ).toThrow();
  }
});
