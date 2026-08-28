<script setup lang="ts">
import {
  Activity,
  Bot,
  Clock3,
  FileStack,
  FolderKanban,
  Gauge,
  Home,
  KeyRound,
  Menu,
  PlugZap,
  Search,
  Settings,
  Sparkles,
  UsersRound,
  Workflow,
} from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { buildBreadcrumbs, type BreadcrumbLabels } from "@/app/breadcrumbs";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { useSessionStore } from "@/features/session/store";
import { selectProjectRef } from "@/shared/project-context";
import {
  currentLocale,
  persistLocale,
  type SupportedLocale,
} from "@/shared/locale";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import CurrentUserSummary from "@/shared/ui/CurrentUserSummary.vue";

const route = useRoute();
const router = useRouter();
const platform = usePlatformStore();
const realtime = useRealtimeStore();
const session = useSessionStore();
const { locale, t } = useI18n();
const mobileOpen = ref(false);
const online = ref(navigator.onLine);
const search = ref("");
const searchInput = ref<HTMLInputElement>();
const searchOpen = ref(false);
const mobileSearchOpen = ref(false);
let disposed = false;

const projectRef = computed(() => {
  const value = route.params.projectRef;
  return typeof value === "string" ? value : undefined;
});
const project = computed(() =>
  projectRef.value ? platform.projects[projectRef.value] : undefined,
);
const pendingCount = computed(
  () => platform.gateList.filter((item) => item.state === "OPEN").length,
);
const connectionState = computed(() => {
  if (!online.value || realtime.platformState.state === "offline")
    return "DEGRADED";
  if (realtime.platformState.state === "live") return "CONNECTED";
  if (realtime.platformState.state === "recovering") return "RECOVERING";
  return "CONNECTING";
});
const breadcrumbs = computed(() => {
  const agentRef =
    typeof route.params.agentRef === "string"
      ? route.params.agentRef
      : undefined;
  const workflowRef =
    typeof route.params.workflowRef === "string"
      ? route.params.workflowRef
      : undefined;
  const runRef =
    typeof route.params.runRef === "string" ? route.params.runRef : undefined;
  const labels: BreadcrumbLabels = {
    home: t("nav.home"),
    onboarding: t("nav.onboarding"),
    assistant: t("nav.assistant"),
    projects: t("nav.projects"),
    project: t("app.project"),
    agents: t("nav.agents"),
    agent: t("nav.agent"),
    workflows: t("nav.workflows"),
    workflow: t("nav.workflow"),
    newRun: t("nav.newRun"),
    runs: t("nav.runs"),
    run: t("nav.run"),
    files: t("nav.files"),
    automations: t("nav.automations"),
    integrations: t("nav.integrations"),
    decisions: t("nav.decisions"),
    administration: t("nav.administration"),
    access: t("nav.access"),
    audit: t("nav.audit"),
  };
  return buildBreadcrumbs(
    {
      routeName: typeof route.name === "string" ? route.name : undefined,
      ...(project.value
        ? { project: { ref: project.value.ref, name: project.value.name } }
        : {}),
      ...(agentRef && platform.agents[agentRef]
        ? { agentName: platform.agents[agentRef].name }
        : {}),
      ...(workflowRef && platform.workflows[workflowRef]
        ? { workflowName: platform.workflows[workflowRef].name }
        : {}),
      ...(runRef && platform.runs[runRef]
        ? { runName: platform.runs[runRef].title }
        : {}),
    },
    labels,
  );
});

const globalLinks = computed(() => [
  { name: "home", label: t("nav.home"), path: "/", icon: Home },
  {
    name: "projects",
    label: t("nav.projects"),
    path: "/projects",
    icon: FolderKanban,
  },
  { name: "runs", label: t("nav.runs"), path: "/runs", icon: Activity },
  {
    name: "decisions",
    label: t("nav.decisions"),
    path: "/decisions",
    count: pendingCount.value,
    icon: KeyRound,
  },
  {
    name: "integrations",
    label: t("nav.integrations"),
    path: "/integrations",
    icon: PlugZap,
  },
]);
const administrationLink = computed(() => ({
  label: t("nav.administration"),
  path: "/administration",
  icon: Settings,
}));
const projectLinks = computed(() => {
  if (!projectRef.value) return [];
  const prefix = `/projects/${encodeURIComponent(projectRef.value)}`;
  return [
    {
      name: "project",
      label: t("nav.overview"),
      path: prefix,
      icon: Gauge,
    },
    {
      name: "agents",
      label: t("nav.agents"),
      path: `${prefix}/agents`,
      icon: Bot,
    },
    {
      name: "workflows",
      label: t("nav.workflows"),
      path: `${prefix}/workflows`,
      icon: Workflow,
    },
    {
      name: "project-runs",
      label: t("nav.runs"),
      path: `${prefix}/runs`,
      icon: Activity,
    },
    {
      name: "files",
      label: t("nav.files"),
      path: `${prefix}/files`,
      icon: FileStack,
    },
    {
      name: "automations",
      label: t("nav.automations"),
      path: `${prefix}/automations`,
      icon: Clock3,
    },
    {
      name: "project-access",
      label: t("nav.members"),
      path: `${prefix}/members`,
      icon: UsersRound,
    },
  ];
});

function changeProject(event: Event): void {
  const ref = (event.target as HTMLSelectElement).value;
  if (ref) void router.push(`/projects/${encodeURIComponent(ref)}`);
  else void router.push("/projects");
}

async function submitSearch(): Promise<void> {
  if (search.value.trim().length < 2) {
    searchOpen.value = true;
    return;
  }
  searchOpen.value = true;
  mobileOpen.value = false;
  await platform.search(search.value);
}

function openMobileSearch(): void {
  mobileSearchOpen.value = true;
  searchOpen.value = false;
  void nextTick(() => searchInput.value?.focus());
}

function closeSearch(): void {
  searchOpen.value = false;
  mobileSearchOpen.value = false;
}

function searchResultPath(result: {
  kind: "PROJECT" | "AGENT" | "WORKFLOW" | "RUN";
  ref: string;
  projectRef?: string;
}): string {
  if (result.kind === "PROJECT") return `/projects/${result.ref}`;
  if (result.kind === "RUN") return `/runs/${result.ref}`;
  const project = encodeURIComponent(result.projectRef ?? "");
  const resource = result.kind === "AGENT" ? "agents" : "workflows";
  return `/projects/${project}/${resource}/${encodeURIComponent(result.ref)}`;
}

function changeLocale(value: SupportedLocale): void {
  persistLocale(value);
  locale.value = value;
}

function setOnline(): void {
  online.value = navigator.onLine;
}

watch(
  projectRef,
  (value) => {
    selectProjectRef(value);
    if (value && !platform.projects[value]) void platform.loadProject(value);
  },
  { immediate: true },
);
watch(
  () => route.fullPath,
  () => {
    mobileOpen.value = false;
    closeSearch();
  },
);
onMounted(() => {
  locale.value = currentLocale();
  document.documentElement.lang = locale.value;
  window.addEventListener("online", setOnline);
  window.addEventListener("offline", setOnline);
  void Promise.all([
    platform.loadProjects(),
    platform.loadGates(),
    platform.loadBootstrap(),
  ]).finally(() => {
    if (!disposed) realtime.openPlatform();
  });
});
onBeforeUnmount(() => {
  disposed = true;
  window.removeEventListener("online", setOnline);
  window.removeEventListener("offline", setOnline);
  realtime.closePlatform();
  platform.clearOwnerState();
});
</script>

<template>
  <div class="app-shell" @keydown.esc="mobileOpen = false">
    <a class="skip-link" href="#main-content">{{
      $t("common.skipToContent")
    }}</a>
    <header class="topbar">
      <button
        class="icon-button mobile-only"
        type="button"
        :aria-label="$t('app.menu')"
        :aria-expanded="mobileOpen"
        aria-controls="primary-navigation"
        @click="mobileOpen = !mobileOpen"
      >
        <Menu :size="20" aria-hidden="true" />
      </button>
      <RouterLink class="brand" to="/" aria-label="Kodex">
        <span class="brand-mark" aria-hidden="true">
          <img src="/logo.png" alt="" /> </span
        ><span>Kodex</span>
      </RouterLink>
      <div class="topbar-project-switcher desktop-only">
        <label class="sr-only" for="topbar-project-switcher">{{
          $t("app.project")
        }}</label>
        <FolderKanban :size="17" aria-hidden="true" />
        <select
          id="topbar-project-switcher"
          :value="projectRef ?? ''"
          :title="project?.name ?? $t('app.chooseProject')"
          @change="changeProject"
        >
          <option value="">{{ $t("app.chooseProject") }}</option>
          <option
            v-for="item in platform.projectList"
            :key="item.ref"
            :value="item.ref"
          >
            {{ item.name }}
          </option>
        </select>
      </div>
      <form
        class="global-search"
        :class="{ 'global-search--open': mobileSearchOpen }"
        role="search"
        @submit.prevent="submitSearch"
      >
        <label class="sr-only" for="global-search">{{
          $t("app.search")
        }}</label>
        <input
          id="global-search"
          ref="searchInput"
          v-model="search"
          type="search"
          :placeholder="$t('app.search')"
        />
      </form>
      <button
        class="icon-button mobile-only"
        type="button"
        :aria-label="$t('app.openSearch')"
        :aria-expanded="mobileSearchOpen"
        @click="openMobileSearch"
      >
        <Search :size="19" aria-hidden="true" />
      </button>
      <section
        v-if="searchOpen"
        class="global-search-results"
        :aria-label="$t('app.searchResults')"
        aria-live="polite"
        @keydown.esc="closeSearch"
      >
        <header>
          <strong>{{ $t("app.searchResults") }}</strong>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('app.closeSearch')"
            @click="closeSearch"
          >
            ×
          </button>
        </header>
        <p v-if="search.trim().length < 2" class="muted">
          {{ $t("app.searchHint") }}
        </p>
        <p v-else-if="platform.loading.search" class="muted" role="status">
          {{ $t("common.loading") }}
        </p>
        <template v-else-if="platform.problems.search">
          <p role="alert">{{ platform.problems.search.title }}</p>
          <button class="button" type="button" @click="submitSearch">
            {{ $t("common.retry") }}
          </button>
        </template>
        <p v-else-if="platform.searchResults.length === 0" class="muted">
          {{ $t("app.searchEmpty") }}
        </p>
        <div v-else class="search-results-list">
          <RouterLink
            v-for="result in platform.searchResults"
            :key="`${result.kind}:${result.ref}`"
            class="search-result"
            :to="searchResultPath(result)"
            @click="closeSearch"
          >
            <span>
              <small>{{ $t(`app.searchKind.${result.kind}`) }}</small>
              <strong>{{ result.title }}</strong>
              <span>{{ result.subtitle }}</span>
            </span>
            <StatusBadge :state="result.state" />
          </RouterLink>
        </div>
      </section>
      <RouterLink class="decision-link" to="/decisions">
        <KeyRound :size="17" aria-hidden="true" />
        <span class="decision-link__label">{{ $t("nav.decisions") }}</span>
        <span v-if="pendingCount" class="count-badge">{{ pendingCount }}</span>
      </RouterLink>
      <StatusBadge
        class="connection-badge"
        :state="connectionState"
        aria-live="polite"
      />
      <CurrentUserSummary
        v-if="platform.bootstrap"
        :user="platform.bootstrap.currentUser"
        :platform-role="platform.bootstrap.platformRole"
        :locale="locale as SupportedLocale"
        :can-logout="session.canLogout"
        @change-locale="changeLocale"
        @logout="session.logout"
      />
    </header>

    <div v-if="!online" class="offline-banner" role="status">
      {{ $t("app.offline") }}
    </div>
    <div
      v-else-if="realtime.platformState.problemTitle"
      class="offline-banner"
      role="status"
    >
      {{ realtime.platformState.problemTitle }}
    </div>

    <button
      v-if="mobileOpen"
      class="sidebar-backdrop mobile-only"
      type="button"
      :aria-label="$t('common.close')"
      @click="mobileOpen = false"
    />
    <aside
      id="primary-navigation"
      class="sidebar"
      :class="{ 'sidebar--open': mobileOpen }"
    >
      <RouterLink
        class="assistant-entry"
        :to="{ path: '/assistant', query: projectRef ? { projectRef } : {} }"
      >
        <span class="assistant-entry__mark" aria-hidden="true"
          ><Sparkles :size="21"
        /></span>
        <span
          ><strong>{{ $t("app.assistantShort") }}</strong
          ><small>{{ $t("assistant.system") }}</small></span
        >
        <StatusBadge
          v-if="platform.assistant"
          :state="platform.assistant.runtimeState"
        />
      </RouterLink>
      <nav :aria-label="$t('app.navigation')">
        <RouterLink
          v-for="link in globalLinks"
          :key="link.name"
          :to="link.path"
          class="nav-link"
        >
          <span class="nav-link__label"
            ><component :is="link.icon" :size="17" aria-hidden="true" />
            <span>{{ link.label }}</span></span
          ><span v-if="link.count" class="count-badge">{{ link.count }}</span>
        </RouterLink>
      </nav>
      <div class="project-switcher project-switcher--sidebar mobile-only">
        <label for="sidebar-project-switcher">{{ $t("app.project") }}</label>
        <select
          id="sidebar-project-switcher"
          :value="projectRef ?? ''"
          :title="project?.name ?? $t('app.chooseProject')"
          @change="changeProject"
        >
          <option value="">{{ $t("app.chooseProject") }}</option>
          <option
            v-for="item in platform.projectList"
            :key="item.ref"
            :value="item.ref"
          >
            {{ item.name }}
          </option>
        </select>
      </div>
      <nav
        v-if="projectLinks.length"
        class="project-nav"
        :aria-label="$t('app.projectNavigation')"
      >
        <p :title="project?.name ?? $t('app.project')">
          {{ project?.name ?? $t("app.project") }}
        </p>
        <RouterLink
          v-for="link in projectLinks"
          :key="link.name"
          :to="link.path"
          class="nav-link nav-link--project"
          ><span class="nav-link__label"
            ><component :is="link.icon" :size="16" aria-hidden="true" />
            <span>{{ link.label }}</span></span
          ></RouterLink
        >
      </nav>
      <div class="sidebar-footer">
        <RouterLink
          :to="administrationLink.path"
          class="nav-link nav-link--administration"
        >
          <span class="nav-link__label"
            ><component
              :is="administrationLink.icon"
              :size="17"
              aria-hidden="true"
            />
            <span>{{ administrationLink.label }}</span></span
          >
        </RouterLink>
      </div>
    </aside>

    <div id="main-content" class="app-content">
      <nav class="breadcrumbs" :aria-label="$t('app.breadcrumbs')">
        <ol>
          <li v-for="item in breadcrumbs" :key="`${item.path}:${item.label}`">
            <RouterLink v-if="item.path" :to="item.path" :title="item.label">{{
              item.label
            }}</RouterLink>
            <span v-else :title="item.label" aria-current="page">{{
              item.label
            }}</span>
          </li>
        </ol>
      </nav>
      <RouterView />
    </div>

    <nav class="mobile-tabs" :aria-label="$t('app.navigation')">
      <RouterLink to="/"
        ><Home :size="18" aria-hidden="true" /><span>{{
          $t("nav.home")
        }}</span></RouterLink
      >
      <RouterLink to="/projects"
        ><FolderKanban :size="18" aria-hidden="true" /><span>{{
          $t("nav.projects")
        }}</span></RouterLink
      >
      <RouterLink to="/assistant"
        ><Sparkles :size="18" aria-hidden="true" /><span>{{
          $t("app.assistantShort")
        }}</span></RouterLink
      >
      <RouterLink to="/runs"
        ><Activity :size="18" aria-hidden="true" /><span>{{
          $t("nav.runs")
        }}</span></RouterLink
      >
      <RouterLink to="/decisions"
        ><KeyRound :size="18" aria-hidden="true" /><span>{{
          $t("nav.decisions")
        }}</span></RouterLink
      >
    </nav>
  </div>
</template>
