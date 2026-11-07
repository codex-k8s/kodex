import { afterEach, describe, expect, it, vi } from "vitest";
import { normalizeProblem } from "./problem";
import { mutateWithRetry } from "./mutation";
import { resetOwnerRequests } from "./owner-lifetime";

const problem = {
  type: "urn:kodex:problem:integration_credentials_required",
  title: "Настройте личные подключения",
  status: 409,
  code: "INTEGRATION_CREDENTIALS_REQUIRED",
  correlationId: "10000000-0000-4000-8000-000000000001",
  retryable: false,
  integrationCredentialIssues: [
    {
      connectionRef: "int_fixture01",
      connectionName: "GitLab для проверки",
      capabilityKey: "gitlab.project.metadata.read",
      problem: "CREDENTIAL_REVOKED",
    },
    {
      connectionRef: "int_fixture02",
      connectionName: "Jira для проверки",
      capabilityKey: "jira.issue.read",
      problem: "CREDENTIAL_MISSING",
    },
  ],
};
afterEach(() => {
  vi.unstubAllGlobals();
  resetOwnerRequests();
});

describe("полный список личных preflight проблем", () => {
  it("сохраняет полный проверенный список с понятными именами", () => {
    const value = normalizeProblem(
      problem,
      new Response(null, { status: 409 }),
    );
    expect(value.integrationCredentialIssues).toEqual(
      problem.integrationCredentialIssues,
    );
    expect(value.retryable).toBe(false);
    expect(value.kind).toBe("conflict");
  });
  it.each([
    { ...problem, integrationCredentialIssues: [] },
    {
      ...problem,
      integrationCredentialIssues: [
        ...problem.integrationCredentialIssues,
        problem.integrationCredentialIssues[0],
      ],
    },
    {
      ...problem,
      integrationCredentialIssues: [
        { ...problem.integrationCredentialIssues[0], problem: "UNKNOWN" },
      ],
    },
    {
      ...problem,
      integrationCredentialIssues: [
        { ...problem.integrationCredentialIssues[0], secretRef: "foreign" },
      ],
    },
    {
      ...problem,
      integrationCredentialIssues: [
        {
          ...problem.integrationCredentialIssues[0],
          connectionRef: "https://other.example.invalid",
        },
      ],
    },
    {
      ...problem,
      integrationCredentialIssues: [
        {
          ...problem.integrationCredentialIssues[0],
          connectionName: "invalid\nname",
        },
      ],
    },
    { ...problem, status: 500 },
    { ...problem, retryable: true },
  ])("отклоняет malformed список без частичной проекции", (input) => {
    const value = normalizeProblem(
      input,
      new Response(null, { status: input.status }),
    );
    expect(value.code).toBe("INVALID_UPSTREAM_RESPONSE");
    expect(value.integrationCredentialIssues).toEqual([]);
    expect(value.retryable).toBe(false);
  });
  it("не принимает credential diagnostics под чужим кодом ошибки", () => {
    expect(
      normalizeProblem({ ...problem, code: "STATE_CONFLICT" })
        .integrationCredentialIssues,
    ).toEqual([]);
  });
  it("не повторяет launch mutation после отрицательного preflight", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"p".repeat(43)}`,
    });
    const request = vi.fn().mockResolvedValue({
      error: problem,
      response: new Response(null, { status: 409 }),
    });
    await expect(
      mutateWithRetry(request, undefined, "synthetic-preflight-launch"),
    ).rejects.toMatchObject({
      code: "INTEGRATION_CREDENTIALS_REQUIRED",
      retryable: false,
    });
    expect(request).toHaveBeenCalledOnce();
  });
});
