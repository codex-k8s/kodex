import { describe, expect, it, vi } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { validKubernetesChildGate } from "./kubernetes-child-gate";
import { hasValidGateScope } from "./gate-scope";
import { kubernetesChildGateMessages } from "./kubernetes-child-gate-messages";
import KubernetesChildGatePanel from "./components/KubernetesChildGatePanel.vue";
const sdk = vi.hoisted(() => ({ getOwnerGate: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
import { readAddressedGate } from "./gate-navigation";
const gate: OwnerGate = {
  ref: "gat_fixture01",
  version: 1,
  scopeKind: "PROJECT",
  organizationRef: "org_fixture01",
  projectRef: "prj_fixture01",
  runRef: "run_fixture01",
  nodeRef: "nod_fixture01",
  title: "Решение",
  contextSummary: "Контекст",
  consequencesSummary: "Последствия",
  requestedBy: { ref: "usr_fixture01", displayName: "Владелец" },
  state: "OPEN",
  allowedDecisions: ["APPROVE", "REJECT", "CANCEL"],
  decisionConsequences: [],
  openedAt: "2026-11-15T00:00:00Z",
  nextActions: ["RESOLVE_GATE"],
  kubernetesChild: {
    planRef: "kpl_fixture01",
    childRef: "kch_fixture01",
    operation: "kubernetes.patch",
    planDigest: "a".repeat(64),
    inputDigest: "b".repeat(64),
    preview: {
      inputDigest: "b".repeat(64),
      options: [],
      changes: [
        {
          operation: "replace",
          path: "/spec/replicas",
          from: "",
          valueKind: "NUMBER",
          valueJson: "0",
          redacted: false,
        },
      ],
      secretMaterialRef: "",
      limitations: [],
    },
    targetRef: "ktg_fixture01",
    targetRevision: 3,
    intendedName: "",
    intendedGenerateName: "",
    resource: {
      group: "apps",
      version: "v1",
      resource: "deployments",
      namespace: "example",
      name: "selected",
      uid: "30000000-0000-4000-8000-000000000001",
      resourceVersion: "17",
      subresource: "",
      namespaced: true,
      allNamespaces: false,
    },
  },
};
async function html(value: OwnerGate) {
  const app = createSSRApp({
    render: () => h(KubernetesChildGatePanel, { gate: value }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: { kubernetesChildGate: kubernetesChildGateMessages.ru } },
    }),
  );
  return renderToString(app);
}
describe("Kubernetes child Gate response boundary", () => {
  it("shows one exact resource and original pins without turning approval into completed execution", async () => {
    expect(validKubernetesChildGate(gate)).toBe(true);
    expect(hasValidGateScope(gate, "org_fixture01")).toBe(true);
    const output = await html(gate);
    for (const text of [
      "kubernetes.patch",
      "apps/v1/deployments",
      "example",
      "selected",
      "30000000-0000-4000-8000-000000000001",
      "17",
      "kpl_fixture01",
      "kch_fixture01",
      "ktg_fixture01",
      "a".repeat(64),
      "b".repeat(64),
    ])
      expect(output).toContain(text);
    expect(output).toContain(
      "Одобрение относится к одному действию исходного плана",
    );
  });
  it("CREATE keeps collection without a fake actual name, UID or resource version", async () => {
    const value = structuredClone(gate);
    if (!value.kubernetesChild) throw new Error("Fixture child absent");
    value.kubernetesChild.operation = "kubernetes.create";
    Object.assign(value.kubernetesChild.resource, {
      name: "",
      uid: "",
      resourceVersion: "",
    });
    value.kubernetesChild.intendedGenerateName = "generated-";
    expect(validKubernetesChildGate(value)).toBe(true);
    const output = await html(value);
    expect(output).toContain("generated-");
    expect(output).toContain("Окончательное имя назначит Kubernetes");
    expect(output).not.toContain("ResourceVersion");
    expect(output).not.toContain("30000000-0000-4000-8000-000000000001");
  });
  it("RUN shows exact intended Pod name without inventing a persisted incarnation", async () => {
    const value = structuredClone(gate);
    const child = value.kubernetesChild;
    if (!child) throw new Error("Fixture child absent");
    child.operation = "kubernetes.run";
    Object.assign(child.resource, {
      group: "",
      version: "v1",
      resource: "pods",
      name: "",
      uid: "",
      resourceVersion: "",
    });
    child.intendedName = "worker";
    expect(validKubernetesChildGate(value)).toBe(true);
    const output = await html(value);
    expect(output).toContain("worker");
    expect(output).toContain("kubernetes.run");
    expect(output).not.toContain("Окончательное имя назначит Kubernetes");
    child.resource.resource = "configmaps";
    expect(validKubernetesChildGate(value)).toBe(false);
    child.resource.resource = "pods";
    child.intendedName = "";
    child.intendedGenerateName = "worker-";
    expect(validKubernetesChildGate(value)).toBe(false);
    child.intendedName = "worker";
    child.intendedGenerateName = "";
    child.resource.name = "worker";
    expect(validKubernetesChildGate(value)).toBe(false);
  });
  it("invalid or mixed summaries cannot become an ordinary resolvable Gate or leak extra input", async () => {
    for (const mode of [
      "missing",
      "body",
      "targetVersion",
      "namespace",
      "uid",
      "mixed",
      "decision",
      "allNamespaces",
      "create",
    ]) {
      const value = structuredClone(gate);
      const child = value.kubernetesChild;
      if (!child) throw new Error("Fixture child absent");
      switch (mode) {
        case "missing":
          Reflect.deleteProperty(child, "inputDigest");
          break;
        case "body":
          Object.assign(child, { body: "private-synthetic-sentinel" });
          break;
        case "targetVersion":
          child.targetRevision = 0;
          break;
        case "namespace":
          child.resource.namespace = "other/namespace";
          break;
        case "uid":
          child.resource.uid = "";
          break;
        case "mixed":
          Object.assign(value, { integrationIntent: {} });
          break;
        case "decision":
          value.allowedDecisions.push("REQUEST_CHANGES");
          break;
        case "allNamespaces":
          child.resource.allNamespaces = true;
          break;
        case "create":
          child.operation = "kubernetes.create";
          child.intendedName = "another";
          break;
      }
      expect(validKubernetesChildGate(value), mode).toBe(false);
      expect(hasValidGateScope(value, "org_fixture01"), mode).toBe(false);
      const output = await html(value);
      expect(output).toContain("Сведения о действии недоступны");
      expect(output).not.toContain("private-synthetic-sentinel");
      sdk.getOwnerGate.mockResolvedValue({
        data: value,
        response: new Response(),
      });
      await expect(
        readAddressedGate(
          value.ref,
          value.projectRef,
          new AbortController().signal,
          value.organizationRef,
        ),
      ).rejects.toThrow();
    }
  });
  it("rejects misleading preview data and exposes typed submitted values with explicit limitations", async () => {
    for (const mode of [
      "digest",
      "missing",
      "type",
      "hiddenValue",
      "extra",
      "authorizedValues",
    ] as const) {
      const value = structuredClone(gate);
      const child = value.kubernetesChild;
      if (!child) throw new Error("Fixture child absent");
      const change = child.preview.changes[0];
      if (!change) throw new Error("Fixture change absent");
      if (mode === "digest") child.preview.inputDigest = "c".repeat(64);
      if (mode === "missing") Reflect.deleteProperty(child, "preview");
      if (mode === "type") change.valueKind = "OBJECT";
      if (mode === "hiddenValue") change.redacted = true;
      if (mode === "extra")
        Object.assign(child.preview, { body: "private-sentinel" });
      if (mode === "authorizedValues")
        child.preview.limitations = ["NOT_AUTHORIZED"];
      expect(validKubernetesChildGate(value), mode).toBe(false);
      const output = await html(value);
      expect(output).toContain("Сведения о действии недоступны");
      expect(output).not.toContain("private-sentinel");
    }
    const value = structuredClone(gate);
    if (!value.kubernetesChild) throw new Error("Fixture child absent");
    value.kubernetesChild.preview.limitations = [
      "SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN",
    ];
    const output = await html(value);
    expect(output).toContain("/spec/replicas");
    expect(output).toContain("это не полный diff");
    expect(output).toContain("Часть полей скрыта");
  });
  it("shows the server-derived bounded logical PF scope and rejects missing, foreign or caller-shaped policy disclosure", async () => {
    const value = structuredClone(gate);
    if (!value.kubernetesChild) throw new Error("Fixture child absent");
    value.kubernetesChild.operation = "kubernetes.port-forward";
    Object.assign(value.kubernetesChild.resource, {
      group: "",
      version: "v1",
      resource: "pods",
      subresource: "",
      namespaced: true,
    });
    value.kubernetesChild.preview.changes = [];
    value.kubernetesChild.preview.options = [
      {
        key: "ports",
        kind: "ARRAY",
        valueJson: '[{"port":18080,"targetPort":8080}]',
        redacted: false,
      },
      {
        key: "timeoutSeconds",
        kind: "NUMBER",
        valueJson: "30",
        redacted: false,
      },
    ];
    value.kubernetesChild.preview.limitations = [
      "PORT_FORWARD_LOGICAL_STREAM_16_ACTIVE",
    ];
    expect(validKubernetesChildGate(value)).toBe(true);
    const output = await html(value);
    expect(output).toContain(
      "Повторные и последовательные соединения разрешены",
    );
    expect(output).toContain("не более 16");
    expect(output).toContain("не доказывает завершение удалённой стороны");
    for (const mode of [
      "missing",
      "foreign",
      "callerOption",
      "resource",
      "unauthorized",
    ]) {
      const hostile = structuredClone(value);
      if (!hostile.kubernetesChild) throw new Error("Fixture child absent");
      const child = hostile.kubernetesChild;
      if (mode === "missing") child.preview.limitations = [];
      if (mode === "foreign") child.operation = "kubernetes.exec";
      if (mode === "callerOption")
        child.preview.options.push({
          key: "maximumActiveConnections",
          kind: "NUMBER",
          valueJson: "999",
          redacted: false,
        });
      if (mode === "resource") child.resource.resource = "deployments";
      if (mode === "unauthorized") {
        child.preview.options = [];
        child.preview.limitations.push("NOT_AUTHORIZED");
      }
      expect(validKubernetesChildGate(hostile), mode).toBe(false);
    }
  });
  it("ordinary Gate stays available; absent Kubernetes data does not invent a child", () => {
    const value = structuredClone(gate);
    delete value.kubernetesChild;
    expect(validKubernetesChildGate(value)).toBe(true);
    expect(hasValidGateScope(value, "org_fixture01")).toBe(true);
  });
});
