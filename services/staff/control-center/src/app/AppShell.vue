<script setup lang="ts">
import {
  Activity,
  Boxes,
  BookOpenText,
  BriefcaseBusiness,
  CloudCog,
  DatabaseBackup,
  Gauge,
  GitCompareArrows,
  Languages,
  LayoutDashboard,
  LogOut,
  Menu,
  Moon,
  Play,
  PlugZap,
  ScrollText,
  Search,
  Sun,
  UserRoundCog,
  X,
  Zap,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { useProjectsStore } from "@/features/projects/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { usePwaUpdateStore } from "@/features/pwa-update/store";
import { useSessionStore } from "@/features/session/store";
import type { ResourceKind } from "@/shared/api/generated/openapi/types.gen";
import { runtimeConfig } from "@/shared/config/runtime";
import { setMutationGuard } from "@/shared/lib/identity";
import {
  projectReference,
  selectedProjectReference,
  setProjectReference,
} from "@/shared/lib/project-scope";
import { resourceKinds } from "@/shared/lib/resources";
import { subscribeRealtimeDisconnect } from "@/shared/realtime/snapshot-bus";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";

const route = useRoute();
const router = useRouter();
const { locale, t } = useI18n();
const realtime = useRealtimeStore();
const pwaUpdate = usePwaUpdateStore();
const session = useSessionStore();
const projects = useProjectsStore();
const menuOpen = ref(false);
const scopeReady = ref(false);
const searchQuery = ref("");
const searchKind = ref<ResourceKind | "ALL">("ALL");
const theme = ref(localStorage.getItem("mattercodex.theme") ?? "system");
let unsubscribeRealtimeDisconnect: (() => void) | null = null;

const navigation = [
  { to: "/", label: "nav.overview", icon: LayoutDashboard, project: false },
  {
    to: "/workspaces",
    label: "nav.workspaces",
    icon: BriefcaseBusiness,
    project: false,
  },
  { to: "/people", label: "nav.people", icon: UserRoundCog, project: true },
  {
    to: "/instructions",
    label: "nav.instructions",
    icon: BookOpenText,
    project: true,
  },
  { to: "/providers", label: "nav.providers", icon: CloudCog, project: true },
  {
    to: "/integrations",
    label: "nav.integrations",
    icon: PlugZap,
    project: true,
  },
  { to: "/role-images", label: "nav.roleImages", icon: Boxes, project: true },
  { to: "/automations", label: "nav.automations", icon: Zap, project: true },
  { to: "/runs", label: "nav.runs", icon: Play, project: true },
  {
    to: "/operations/incidents",
    label: "nav.incidents",
    icon: Activity,
    project: true,
  },
  {
    to: "/operations/backups",
    label: "nav.backups",
    icon: DatabaseBackup,
    project: true,
  },
  {
    to: "/operations/audit",
    label: "nav.audit",
    icon: ScrollText,
    project: true,
  },
  {
    to: "/operations/configuration",
    label: "nav.configuration",
    icon: GitCompareArrows,
    project: true,
  },
  {
    to: "/operations/diagnostics",
    label: "nav.diagnostics",
    icon: Gauge,
    project: true,
  },
];

const hasProject = computed(() => selectedProjectReference.value !== null);
const routeRequiresProject = computed(
  () => route.name !== "overview" && route.name !== "workspaces",
);

const themeIcon = computed(() =>
  theme.value === "dark" ? Moon : theme.value === "light" ? Sun : Gauge,
);

function applyTheme(): void {
  document.documentElement.dataset.theme = theme.value;
  localStorage.setItem("mattercodex.theme", theme.value);
}

function applyPwaUpdate(): void {
  const registration = pwaUpdate.registration;
  if (!registration?.waiting) return;
  navigator.serviceWorker.addEventListener(
    "controllerchange",
    () => window.location.reload(),
    { once: true },
  );
  registration.waiting.postMessage({ type: "SKIP_WAITING" });
}

function verifySessionAfterRealtimeClose(): void {
  void session.verify();
}

function setLocale(value: string): void {
  locale.value = value === "en" ? "en" : "ru";
  document.documentElement.lang = locale.value;
  localStorage.setItem("mattercodex.locale", locale.value);
}

async function submitSearch(): Promise<void> {
  if (!hasProject.value) return;
  const query = searchQuery.value.trim();
  if (query.length < 2) return;
  await router.push({
    name: "search",
    query: { kind: searchKind.value, query },
  });
  menuOpen.value = false;
}

async function initializeScope(): Promise<void> {
  scopeReady.value = false;
  realtime.stop();
  await projects.load();
  if (
    projects.projects.phase !== "ready" &&
    projects.projects.phase !== "empty"
  )
    return;
  const available = new Set(projects.projects.data.map((item) => item.id));
  const routeReference =
    typeof route.params.projectId === "string" ? route.params.projectId : null;
  const storedReference = projectReference();
  const selected =
    (routeReference && available.has(routeReference) ? routeReference : null) ??
    (storedReference && available.has(storedReference)
      ? storedReference
      : null) ??
    projects.projects.data[0]?.id ??
    null;
  setProjectReference(selected);
  if (route.name === "workspace" && routeReference !== selected) {
    await router.replace(
      selected
        ? { name: "workspace", params: { projectId: selected } }
        : { name: "workspaces" },
    );
  }
  scopeReady.value = true;
  if (selected) realtime.start();
}

function switchProject(value: string): void {
  if (!projects.projects.data.some((item) => item.id === value)) return;
  setProjectReference(value);
  const target =
    route.name === "workspace" ? `/workspaces/${value}` : route.fullPath;
  window.location.assign(target);
}

function guardNavigation(projectScoped: boolean, event: MouseEvent): void {
  if (projectScoped && !hasProject.value) event.preventDefault();
}

onMounted(async () => {
  applyTheme();
  setMutationGuard(() => !hasProject.value || realtime.ready);
  unsubscribeRealtimeDisconnect = subscribeRealtimeDisconnect(
    verifySessionAfterRealtimeClose,
  );
  await initializeScope();
});
onBeforeUnmount(() => {
  unsubscribeRealtimeDisconnect?.();
  unsubscribeRealtimeDisconnect = null;
  setMutationGuard(null);
  realtime.stop();
});
</script>

<template>
  <div class="app-shell" :class="{ 'app-shell--offline': !realtime.online }">
    <aside class="sidebar" :class="{ 'sidebar--open': menuOpen }">
      <div class="sidebar__brand">
        <div class="brand-mark" aria-hidden="true">M</div>
        <div>
          <strong>{{ $t("app.name") }}</strong
          ><small>{{ $t("app.controlCenter") }}</small>
        </div>
        <button
          class="icon-button sidebar__close"
          type="button"
          :aria-label="$t('nav.closeMenu')"
          @click="menuOpen = false"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </div>
      <nav class="sidebar__nav" :aria-label="$t('app.controlCenter')">
        <RouterLink
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          :aria-disabled="item.project && !hasProject"
          :class="{ 'router-link-disabled': item.project && !hasProject }"
          @click="
            guardNavigation(item.project, $event);
            menuOpen = false;
          "
        >
          <component :is="item.icon" :size="17" aria-hidden="true" />
          <span>{{ $t(item.label) }}</span>
        </RouterLink>
      </nav>
      <div class="sidebar__account">
        <div class="avatar" aria-hidden="true">M</div>
        <div>
          <strong>{{ $t("app.owner") }}</strong
          ><small>{{ $t("app.ownerRole") }}</small>
        </div>
        <button
          v-if="session.canLogout"
          class="icon-button"
          type="button"
          :aria-label="$t('auth.logout')"
          @click="session.logout"
        >
          <LogOut :size="16" aria-hidden="true" />
        </button>
      </div>
    </aside>
    <button
      v-if="menuOpen"
      class="sidebar-scrim"
      type="button"
      :aria-label="$t('nav.closeMenu')"
      @click="menuOpen = false"
    />

    <div class="app-shell__main">
      <header class="topbar">
        <button
          class="icon-button topbar__menu"
          type="button"
          :aria-label="$t('nav.openMenu')"
          @click="menuOpen = true"
        >
          <Menu :size="19" aria-hidden="true" />
        </button>
        <span class="environment-chip">{{
          $t("app.environment", { name: runtimeConfig().environment })
        }}</span>
        <label
          v-if="projects.projects.data.length"
          class="compact-select project-select"
        >
          <BriefcaseBusiness :size="15" aria-hidden="true" />
          <span class="sr-only">{{ $t("app.workspace") }}</span>
          <select
            :value="selectedProjectReference ?? ''"
            @change="switchProject(($event.target as HTMLSelectElement).value)"
          >
            <option
              v-for="project in projects.projects.data"
              :key="project.id"
              :value="project.id"
            >
              {{ project.name }}
            </option>
          </select>
        </label>
        <form
          class="global-search"
          role="search"
          @submit.prevent="submitSearch"
        >
          <Search :size="15" aria-hidden="true" />
          <label class="sr-only" for="global-search-kind">{{
            $t("search.kind")
          }}</label>
          <select id="global-search-kind" v-model="searchKind">
            <option value="ALL">ALL</option>
            <option v-for="kind in resourceKinds" :key="kind" :value="kind">
              {{ kind }}
            </option>
          </select>
          <label class="sr-only" for="global-search-query">{{
            $t("search.query")
          }}</label>
          <input
            id="global-search-query"
            v-model="searchQuery"
            type="search"
            :placeholder="$t('search.placeholder')"
            minlength="2"
            maxlength="128"
            :disabled="!hasProject"
          />
        </form>
        <div class="topbar__spacer" />
        <div
          class="realtime-indicator"
          :class="{
            'realtime-indicator--offline':
              !realtime.online || !realtime.connected || realtime.replacing,
          }"
        >
          <span aria-hidden="true" />
          {{
            !hasProject
              ? $t("app.workspaceRequired")
              : !realtime.online
                ? $t("app.offline")
                : !realtime.connected
                  ? $t("app.realtimeOffline")
                  : realtime.replacing
                    ? $t("app.realtimeReplacing")
                    : $t("app.realtime")
          }}
          <small v-if="realtime.problemCode">{{ realtime.problemCode }}</small>
        </div>
        <label class="compact-select">
          <Languages :size="15" aria-hidden="true" /><span class="sr-only">{{
            $t("common.locale")
          }}</span>
          <select
            :value="locale"
            @change="setLocale(($event.target as HTMLSelectElement).value)"
          >
            <option value="ru">RU</option>
            <option value="en">EN</option>
          </select>
        </label>
        <label class="compact-select">
          <component :is="themeIcon" :size="15" aria-hidden="true" /><span
            class="sr-only"
            >{{ $t("common.theme") }}</span
          >
          <select v-model="theme" @change="applyTheme">
            <option value="system">{{ t("common.system") }}</option>
            <option value="light">{{ t("common.light") }}</option>
            <option value="dark">{{ t("common.dark") }}</option>
          </select>
        </label>
      </header>
      <div
        v-if="hasProject && !realtime.online"
        class="connectivity-banner"
        role="status"
        aria-live="polite"
      >
        {{ $t("app.offlineNotice") }}
      </div>
      <div
        v-else-if="hasProject && (!realtime.connected || realtime.replacing)"
        class="connectivity-banner connectivity-banner--info"
        role="status"
        aria-live="polite"
      >
        {{ $t("app.replacingNotice") }}
      </div>
      <div
        v-if="pwaUpdate.registration"
        class="connectivity-banner connectivity-banner--update"
        role="status"
      >
        <span>{{ $t("app.updateAvailable") }}</span>
        <button
          class="button button--secondary"
          type="button"
          @click="applyPwaUpdate"
        >
          {{ $t("app.applyUpdate") }}
        </button>
      </div>
      <div
        v-if="pwaUpdate.failed"
        class="connectivity-banner connectivity-banner--info"
        role="status"
      >
        {{ $t("app.updateUnavailable") }}
      </div>
      <main class="content">
        <AsyncPanel
          v-if="!scopeReady"
          :phase="projects.projects.phase"
          :problem="projects.projects.problem"
          @retry="initializeScope"
        />
        <div
          v-else-if="routeRequiresProject && !hasProject"
          class="state-panel state-panel--quiet"
        >
          <p>{{ $t("app.workspaceRequiredNotice") }}</p>
          <RouterLink class="button button--primary" to="/workspaces">
            {{ $t("workspaces.create") }}
          </RouterLink>
        </div>
        <RouterView v-else />
      </main>
    </div>
  </div>
</template>
