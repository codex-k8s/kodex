import { describe, expect, it } from "vitest";
import {
  prepareMailboxSetup,
  rememberMailboxSetup,
  forgetMailboxSetup,
  mailboxSetupAttempts,
  mailboxSetupRecoveryKey,
  type MailboxSetupAttempt,
} from "./personal-email-recovery";

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
describe("квитанции личного mailbox setup", () => {
  it("сохраняет только coordinates/digest без адреса и лишних secret fields", async () => {
    const cache = storage(),
      request = {
        operation: "CREATE" as const,
        connectionRef: "",
        connectionVersion: 0,
      },
      body = { name: "Личный ящик", sender: "owner@example.test" };
    const attempt = await prepareMailboxSetup(request, body);
    rememberMailboxSetup(
      {
        ...attempt,
        password: "synthetic-private",
        body,
      } as MailboxSetupAttempt,
      cache,
    );
    expect(mailboxSetupAttempts(cache)).toEqual([attempt]);
    expect(cache.getItem(mailboxSetupRecoveryKey)).not.toMatch(
      /owner@example|Личный|synthetic-private|password|body/,
    );
    expect(await prepareMailboxSetup(request, body, attempt)).toEqual(attempt);
    await expect(
      prepareMailboxSetup(
        request,
        { ...body, sender: "different@example.test" },
        attempt,
      ),
    ).rejects.toThrow();
    await expect(
      prepareMailboxSetup(request, { ...body, name: "Другой" }, attempt),
    ).rejects.toThrow();
    expect(() =>
      rememberMailboxSetup(
        { ...attempt, key: "12345678-1234-1234-1234-123456789abc" },
        cache,
      ),
    ).toThrow();
    forgetMailboxSetup({ ...attempt, key: "other" }, cache);
    expect(mailboxSetupAttempts(cache)).toEqual([attempt]);
    forgetMailboxSetup(attempt, cache);
    expect(mailboxSetupAttempts(cache)).toEqual([]);
  });
  it("связывает смену service identity и исходную version с прежним предложением", async () => {
    const request = {
        operation: "CONFIGURE" as const,
        connectionRef: "int_mailbox_owner",
        connectionVersion: 3,
      },
      body = {
        policy: "PROJECT_SHARED" as const,
        serviceSubjectRef: "subject_service_one",
      };
    const attempt = await prepareMailboxSetup(request, body);
    await expect(
      prepareMailboxSetup({ ...request, connectionVersion: 4 }, body, attempt),
    ).rejects.toThrow();
    await expect(
      prepareMailboxSetup(
        request,
        { ...body, serviceSubjectRef: "subject_service_two" },
        attempt,
      ),
    ).rejects.toThrow();
  });
  it("отклоняет повреждённое storage и не вытесняет неопределённые записи сверх лимита", () => {
    const cache = storage(),
      attempt: MailboxSetupAttempt = {
        operation: "REVOKE",
        connectionRef: "int_mailbox_owner",
        connectionVersion: 3,
        key: "12345678-1234-1234-1234-123456789abc",
        digest: "a".repeat(64),
      };
    for (const raw of [
      "null",
      "{}",
      JSON.stringify([{ ...attempt, owner: "foreign" }]),
      JSON.stringify([{ ...attempt, connectionVersion: 0 }]),
      JSON.stringify([attempt, attempt]),
    ]) {
      cache.setItem(mailboxSetupRecoveryKey, raw);
      expect(() => mailboxSetupAttempts(cache)).toThrow();
    }
    cache.removeItem(mailboxSetupRecoveryKey);
    for (let index = 0; index < 20; index++)
      rememberMailboxSetup(
        { ...attempt, connectionRef: `int_mailbox_owner_${index}` },
        cache,
      );
    expect(() =>
      rememberMailboxSetup(
        { ...attempt, connectionRef: "int_another_mailbox" },
        cache,
      ),
    ).toThrow();
    expect(mailboxSetupAttempts(cache)).toHaveLength(20);
  });
});
