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
  ownerGeneration: 1,
  kind: "AUTH_SECRET",
  key: "12345678-1234-1234-1234-123456789abc",
};
it("legacy попытка без owner epoch остаётся неопределённой metadata и не может быть новым credential intent", () => {
  const cache = storage();
  const { ownerGeneration, ...legacy } = attempt;
  expect(ownerGeneration).toBe(1);
  cache.setItem(mailboxCredentialRecoveryKey, JSON.stringify([legacy]));
  const old = pendingMailboxCredential(attempt.connectionRef, cache);
  expect(old?.ownerGeneration).toBe(0);
  expect(() =>
    rememberMailboxCredential({ ...attempt, ownerGeneration: 0 }, cache),
  ).toThrow();
  if (!old) throw new Error("Legacy attempt fixture unavailable");
  forgetMailboxCredential(old, cache);
  expect(
    pendingMailboxCredential(attempt.connectionRef, cache),
  ).toBeUndefined();
});
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
