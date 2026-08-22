<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { useSessionStore } from "@/features/session/store";
import { selectProjectRef } from "@/shared/project-context";
import {
  currentLocale,
  persistLocale,
  type SupportedLocale,
} from "@/shared/locale";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const route = useRoute();
const router = useRouter();
const platform = usePlatformStore();
const session = useSessionStore();
const { locale, t } = useI18n();
const mobileOpen = ref(false);
const online = ref(navigator.onLine);
const search = ref("");

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

const globalLinks = computed(() => [
  { name: "home", label: t("nav.home"), path: "/" },
  { name: "projects", label: t("nav.projects"), path: "/projects" },
  { name: "runs", label: t("nav.runs"), path: "/runs" },
  {
    name: "decisions",
    label: t("nav.decisions"),
    path: "/decisions",
    count: pendingCount.value,
  },
  { name: "integrations", label: t("nav.integrations"), path: "/integrations" },
  {
    name: "administration",
    label: t("nav.administration"),
    path: "/administration",
  },
]);
const projectLinks = computed(() => {
  if (!projectRef.value) return [];
  const prefix = `/projects/${encodeURIComponent(projectRef.value)}`;
  return [
    { name: "project", label: t("nav.overview"), path: prefix },
    { name: "agents", label: t("nav.agents"), path: `${prefix}/agents` },
    {
      name: "workflows",
      label: t("nav.workflows"),
      path: `${prefix}/workflows`,
    },
    { name: "project-runs", label: t("nav.runs"), path: `${prefix}/runs` },
    { name: "files", label: t("nav.files"), path: `${prefix}/files` },
    {
      name: "automations",
      label: t("nav.automations"),
      path: `${prefix}/automations`,
    },
    {
      name: "project-access",
      label: t("nav.members"),
      path: `${prefix}/members`,
    },
  ];
});

function changeProject(event: Event): void {
  const ref = (event.target as HTMLSelectElement).value;
  if (ref) void router.push(`/projects/${encodeURIComponent(ref)}`);
  else void router.push("/projects");
}

function submitSearch(): void {
  if (!search.value.trim()) return;
  void router.push({ path: "/projects", query: { q: search.value.trim() } });
  mobileOpen.value = false;
}

function changeLocale(event: Event): void {
  const value = (event.target as HTMLSelectElement).value as SupportedLocale;
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
  ]);
});
onBeforeUnmount(() => {
  window.removeEventListener("online", setOnline);
  window.removeEventListener("offline", setOnline);
});
</script>

<template>
  <div class="app-shell">
    <a class="skip-link" href="#main-content">{{
      $t("common.skipToContent")
    }}</a>
    <header class="topbar">
      <button
        class="icon-button mobile-only"
        type="button"
        :aria-label="$t('app.menu')"
        :aria-expanded="mobileOpen"
        @click="mobileOpen = !mobileOpen"
      >
        ☰
      </button>
      <RouterLink class="brand" to="/" aria-label="MatterCodex">
        <span class="brand-mark" aria-hidden="true">M</span
        ><span>MatterCodex</span>
      </RouterLink>
      <form class="global-search" role="search" @submit.prevent="submitSearch">
        <label class="sr-only" for="global-search">{{
          $t("app.search")
        }}</label>
        <input
          id="global-search"
          v-model="search"
          type="search"
          :placeholder="$t('app.search')"
        />
      </form>
      <RouterLink class="decision-link" to="/decisions">
        {{ $t("nav.decisions")
        }}<span v-if="pendingCount" class="count-badge">{{
          pendingCount
        }}</span>
      </RouterLink>
      <select
        class="locale-select"
        :value="locale"
        :aria-label="$t('common.language')"
        @change="changeLocale"
      >
        <option value="ru">RU</option>
        <option value="en">EN</option>
      </select>
      <button
        v-if="session.canLogout"
        class="button button--ghost desktop-only"
        type="button"
        @click="session.logout"
      >
        {{ $t("auth.logout") }}
      </button>
    </header>

    <div v-if="!online" class="offline-banner" role="status">
      {{ $t("app.offline") }}
    </div>

    <aside class="sidebar" :class="{ 'sidebar--open': mobileOpen }">
      <RouterLink
        class="assistant-entry"
        :to="{ path: '/assistant', query: projectRef ? { projectRef } : {} }"
      >
        <span class="assistant-entry__mark" aria-hidden="true">✦</span>
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
          <span>{{ link.label }}</span
          ><span v-if="link.count" class="count-badge">{{ link.count }}</span>
        </RouterLink>
      </nav>
      <div class="project-switcher">
        <label for="project-switcher">{{ $t("app.project") }}</label>
        <select
          id="project-switcher"
          :value="projectRef ?? ''"
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
        <p>{{ project?.name ?? $t("app.project") }}</p>
        <RouterLink
          v-for="link in projectLinks"
          :key="link.name"
          :to="link.path"
          class="nav-link nav-link--project"
          >{{ link.label }}</RouterLink
        >
      </nav>
    </aside>

    <div id="main-content" class="app-content">
      <div class="breadcrumbs" aria-label="Breadcrumb">
        <RouterLink to="/">{{ $t("nav.home") }}</RouterLink>
        <template v-if="project">
          <span aria-hidden="true">/</span
          ><RouterLink :to="`/projects/${project.ref}`">{{
            project.name
          }}</RouterLink>
        </template>
      </div>
      <RouterView />
    </div>

    <nav class="mobile-tabs" :aria-label="$t('app.navigation')">
      <RouterLink to="/">{{ $t("nav.home") }}</RouterLink>
      <RouterLink to="/projects">{{ $t("nav.projects") }}</RouterLink>
      <RouterLink to="/assistant">{{ $t("app.assistantShort") }}</RouterLink>
      <RouterLink to="/runs">{{ $t("nav.runs") }}</RouterLink>
      <RouterLink to="/decisions">{{ $t("nav.decisions") }}</RouterLink>
    </nav>
  </div>
</template>
