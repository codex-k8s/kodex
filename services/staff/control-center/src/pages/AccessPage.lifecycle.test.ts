import { readFile } from "node:fs/promises";

import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useAccessStore } from "@/features/access/store";
import AccessPage from "@/pages/AccessPage.vue";
import type {
  AccessBinding,
  AccessRole,
  Membership,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";

const route = vi.hoisted(() => ({
  name: "access",
  params: { section: "roles" },
  query: {},
}));
vi.mock("vue-router", () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn() }),
}));

const role: AccessRole = {
  ref: "role_operator",
  version: 2,
  kind: "CUSTOM",
  state: "ACTIVE",
  bindingCount: 1,
  currentVersion: {
    ref: "role_version_operator",
    roleRef: "role_operator",
    revision: 2,
    name: "Оператор",
    description: "Операционная роль",
    permissionKeys: ["project.read"],
    allowedScopes: ["PROJECT"],
    changeComment: "Создана для проверки lifecycle",
    createdAt: "2026-08-31T10:00:00Z",
    createdBy: { ref: "user_owner", displayName: "Владелец" },
  },
  updatedAt: "2026-08-31T10:00:00Z",
};
const binding: AccessBinding = {
  ref: "binding_operator",
  version: 3,
  managementKind: "DIRECT",
  subject: {
    ref: "user_operator",
    kind: "USER",
    displayName: "Оператор",
    active: true,
    oidcGroupRefs: [],
  },
  roleVersion: role.currentVersion,
  scope: { kind: "PROJECT", projectRef: "project_sales" },
  conditions: { requireOwner: false },
  state: "ACTIVE",
  createdAt: "2026-08-31T10:00:00Z",
  updatedAt: "2026-08-31T10:00:00Z",
};

interface AccessSetup {
  addPlatformMember: () => void;
  editPlatformMember: (membership: Membership) => void;
  revokePlatformMember: (membership: Membership) => void;
  platformDialog: { value: boolean };
  archiveRole: (role: AccessRole) => void;
  bindingDialog: { value: boolean };
  confirmMutation: () => Promise<void>;
  confirmation: {
    value?:
      | { kind: "ARCHIVE_ROLE"; role: AccessRole }
      | { kind: "REVOKE_BINDING"; binding: AccessBinding }
      | { kind: "REVOKE_PLATFORM_MEMBERSHIP"; membership: Membership };
  };
  createBinding: () => Promise<void>;
  revokeBinding: (binding: AccessBinding) => void;
}

function deferred(): {
  promise: Promise<void>;
  resolve: () => void;
} {
  let resolve!: () => void;
  const promise = new Promise<void>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

function configureStore() {
  const pinia = createPinia();
  const access = useAccessStore(pinia);
  access.roles = [role];
  access.bindings = [binding];
  const loaders = [
    "loadPermissions",
    "loadProjects",
    "loadRoles",
    "loadGroups",
    "loadIntegrations",
    "loadMembershipPresentation",
    "loadSubjects",
    "loadBindings",
  ] as const;
  for (const loader of loaders) vi.spyOn(access, loader).mockResolvedValue();
  return { access, pinia };
}

async function setupPage() {
  const { access, pinia } = configureStore();
  const i18n = createI18n({
    legacy: false,
    locale: "ru",
    missingWarn: false,
    messages: { ru: {} },
  });
  const setup = (await captureSetupState(AccessPage, (app) => {
    app.use(pinia);
    app.use(i18n);
  })) as unknown as AccessSetup;
  return { access, setup };
}

describe("AccessPage lifecycle confirmations", () => {
  beforeEach(() => vi.clearAllMocks());

  it("архивирует роль и отзывает назначение только после подтверждения", async () => {
    const { access, setup } = await setupPage();
    const archive = vi.spyOn(access, "archiveRole").mockResolvedValue();
    const revoke = vi.spyOn(access, "revokeBinding").mockResolvedValue();

    setup.archiveRole(role);
    expect(archive).not.toHaveBeenCalled();
    expect(setup.confirmation.value).toEqual({ kind: "ARCHIVE_ROLE", role });
    await setup.confirmMutation();
    expect(archive).toHaveBeenCalledOnce();
    expect(archive).toHaveBeenCalledWith(role);
    expect(setup.confirmation.value).toBeUndefined();

    setup.revokeBinding(binding);
    expect(revoke).not.toHaveBeenCalled();
    expect(setup.confirmation.value).toEqual({
      kind: "REVOKE_BINDING",
      binding,
    });
    await setup.confirmMutation();
    expect(revoke).toHaveBeenCalledOnce();
    expect(revoke).toHaveBeenCalledWith(binding);
    expect(setup.confirmation.value).toBeUndefined();
  });

  it("использует ModalDialog вместо window.confirm", async () => {
    const source = await readFile(
      new URL("./AccessPage.vue", import.meta.url),
      "utf8",
    );

    expect(source).toContain('v-if="confirmation"');
    expect(source).toContain("<ModalDialog");
    expect(source).toContain('@click="confirmMutation"');
    expect(source).not.toContain("window.confirm");
  });

  it("открывает новую привязку только после полного каталога ролей", async () => {
    const { access, setup } = await setupPage();
    const catalog = deferred();
    const loadBindingRoles = vi
      .spyOn(access, "loadBindingRoles")
      .mockReturnValue(catalog.promise);

    const open = setup.createBinding();

    expect(loadBindingRoles).toHaveBeenCalledOnce();
    expect(setup.bindingDialog.value).toBe(false);
    catalog.resolve();
    await open;
    expect(setup.bindingDialog.value).toBe(true);
  });

  it("не предлагает изменение или отзыв последнего владельца без server nextActions", async () => {
    const { access, setup } = await setupPage();
    const owner: Membership = {
      ref: "membership_owner",
      version: 1,
      user: { ref: "user_owner", displayName: "Владелец" },
      platformRole: "OWNER",
      permissions: [],
      active: true,
      nextActions: [],
    };
    setup.editPlatformMember(owner);
    setup.revokePlatformMember(owner);
    setup.addPlatformMember();
    expect(setup.platformDialog.value).toBe(false);
    expect(setup.confirmation.value).toBeUndefined();

    access.platformMembershipActions = ["MANAGE_MEMBERS"];
    setup.addPlatformMember();
    expect(setup.platformDialog.value).toBe(true);
    setup.platformDialog.value = false;

    const member = {
      ...owner,
      ref: "membership_other",
      nextActions: ["EDIT", "REVOKE"] as Membership["nextActions"],
    };
    setup.editPlatformMember(member);
    expect(setup.platformDialog.value).toBe(true);
    setup.revokePlatformMember(member);
    expect(setup.confirmation.value).toEqual({
      kind: "REVOKE_PLATFORM_MEMBERSHIP",
      membership: member,
    });
    const revoke = vi
      .spyOn(access, "revokePlatformMembership")
      .mockResolvedValue();
    expect(revoke).not.toHaveBeenCalled();
    await setup.confirmMutation();
    expect(revoke).toHaveBeenCalledWith(member);
  });
});
