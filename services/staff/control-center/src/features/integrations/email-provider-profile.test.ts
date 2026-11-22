import { describe, expect, it } from "vitest";
import { reactive } from "vue";
import {
  mailboxProviderProfile,
  mailboxApprovalProfile,
  emailMailboxOperations,
} from "./email-provider-profile";
import type {
  EmailMailboxProviderProfile,
  EmailMailboxSpecification,
} from "@/shared/api/generated/openapi/types.gen";

describe("явные почтовые профили", () => {
  it("применяет профиль к reactive форме без изменения исходных координат", () => {
    const original = reactive<EmailMailboxSpecification>({
      recipients: ["recipient@example.test"],
      smtp: {
        host: "old.example.test",
        secret: { name: "owned", generation: 2 },
      },
    });
    const next = mailboxProviderProfile(original, "GMAIL_APP_PASSWORD");
    expect(next.smtp?.host).toBe("smtp.gmail.com");
    expect(next.smtp?.secret).toEqual({ name: "owned", generation: 2 });
    expect(next.recipients).toEqual(["recipient@example.test"]);
    expect(original.smtp?.host).toBe("old.example.test");
    expect(original.providerProfile).toBeUndefined();
  });
  it.each(["TIMEWEB_PASSWORD", "GMAIL_APP_PASSWORD"] as const)(
    "%s меняет draft endpoints и сохраняет явные owner coordinates",
    (profile) => {
      const original: EmailMailboxSpecification = {
        sender: "owner@example.test",
        recipients: ["recipient@example.test"],
        folder: "Chosen",
        allowedFolders: ["Chosen"],
        smtp: {
          username: { name: "email-user", generation: 3 },
          secret: { name: "email-secret", generation: 4 },
        },
        imap: { ca: { name: "email-ca", generation: 5 } },
        pop: { host: "old.example.test" },
        policies: [{ operation: "SEND", policy: "DENY" }],
      };
      const value = mailboxProviderProfile(original, profile),
        suffix = profile === "TIMEWEB_PASSWORD" ? "timeweb.ru" : "gmail.com";
      expect(value).toMatchObject({
        providerProfile: profile,
        receiveProtocol: "IMAP",
        sender: original.sender,
        recipients: original.recipients,
        folder: "Chosen",
        allowedFolders: ["Chosen"],
        policies: original.policies,
        smtp: {
          host: `smtp.${suffix}`,
          serverName: `smtp.${suffix}`,
          port: 465,
          tlsMode: "IMPLICIT",
          authMethod: "PASSWORD",
          username: original.smtp?.username,
          secret: original.smtp?.secret,
        },
        imap: {
          host: `imap.${suffix}`,
          serverName: `imap.${suffix}`,
          port: 993,
          tlsMode: "IMPLICIT",
          authMethod: "PASSWORD",
          ca: original.imap?.ca,
        },
      });
      expect(value.pop).toBeUndefined();
      expect(original.pop?.host).toBe("old.example.test");
      expect(JSON.stringify(value)).not.toContain("OAUTHBEARER");
    },
  );
  it("не выдаёт неизвестный OAuth профиль за Gmail app password", () => {
    expect(() =>
      mailboxProviderProfile(
        {},
        "GMAIL_XOAUTH2" as EmailMailboxProviderProfile,
      ),
    ).toThrow();
    expect(
      mailboxProviderProfile(
        { smtp: { host: "explicit.example.test" } },
        "CUSTOM",
      ).smtp?.host,
    ).toBe("explicit.example.test");
  });
  it("предлагает 21 mailbox policy без выдачи grant или scoped consent", () => {
    const value = mailboxApprovalProfile({
      allowedFolders: ["Chosen"],
      recipients: [],
    });
    expect(value.policies).toHaveLength(21);
    expect(new Set(emailMailboxOperations).size).toBe(21);
    expect(
      value.policies
        ?.filter((policy) => policy.policy === "HUMAN_GATE")
        .map((policy) => policy.operation),
    ).toEqual(["SEND", "REPLY", "REPLY_ALL", "FORWARD", "DELETE"]);
    expect(
      value.policies?.every((policy) => policy.folders?.join() === "Chosen"),
    ).toBe(true);
    expect(value.recipients).toEqual([]);
    expect(Object.keys(value).sort()).toEqual([
      "allowedFolders",
      "policies",
      "recipients",
    ]);
  });
});
