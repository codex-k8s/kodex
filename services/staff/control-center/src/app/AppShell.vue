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
import { useRouter } from "vue-router";

import { useRealtimeStore } from "@/features/realtime/store";
import { usePwaUpdateStore } from "@/features/pwa-update/store";
import { useSessionStore } from "@/features/session/store";
import { bindRealtimeSnapshots } from "@/app/realtime-bindings";
import type { ResourceKind } from "@/shared/api/generated/openapi/types.gen";
import { runtimeConfig } from "@/shared/config/runtime";
import { setMutationGuard } from "@/shared/lib/identity";
import { resourceKinds } from "@/shared/lib/resources";

const router = useRouter();
const { locale, t } = useI18n();
const realtime = useRealtimeStore();
const pwaUpdate = usePwaUpdateStore();
const session = useSessionStore();
const menuOpen = ref(false);
const searchQuery = ref("");
const searchKind = ref<ResourceKind | "ALL">("ALL");
const theme = ref(localStorage.getItem("mattercodex.theme") ?? "system");
let unbindRealtime: (() => void) | null = null;

const navigation = [
  { to: "/", label: "nav.overview", icon: LayoutDashboard },
  { to: "/workspaces", label: "nav.workspaces", icon: BriefcaseBusiness },
  { to: "/people", label: "nav.people", icon: UserRoundCog },
  { to: "/instructions", label: "nav.instructions", icon: BookOpenText },
  { to: "/providers", label: "nav.providers", icon: CloudCog },
  { to: "/integrations", label: "nav.integrations", icon: PlugZap },
  { to: "/role-images", label: "nav.roleImages", icon: Boxes },
  { to: "/automations", label: "nav.automations", icon: Zap },
  { to: "/runs", label: "nav.runs", icon: Play },
  { to: "/operations/incidents", label: "nav.incidents", icon: Activity },
  { to: "/operations/backups", label: "nav.backups", icon: DatabaseBackup },
  { to: "/operations/audit", label: "nav.audit", icon: ScrollText },
  {
    to: "/operations/configuration",
    label: "nav.configuration",
    icon: GitCompareArrows,
  },
  { to: "/operations/diagnostics", label: "nav.diagnostics", icon: Gauge },
];

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
  const query = searchQuery.value.trim();
  if (query.length < 2) return;
  await router.push({
    name: "search",
    query: { kind: searchKind.value, query },
  });
  menuOpen.value = false;
}

onMounted(() => {
  applyTheme();
  unbindRealtime = bindRealtimeSnapshots();
  setMutationGuard(() => realtime.ready);
  realtime.start();
  window.addEventListener(
    "mattercodex:realtime-disconnected",
    verifySessionAfterRealtimeClose,
  );
});
onBeforeUnmount(() => {
  window.removeEventListener(
    "mattercodex:realtime-disconnected",
    verifySessionAfterRealtimeClose,
  );
  unbindRealtime?.();
  unbindRealtime = null;
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
          @click="menuOpen = false"
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
            !realtime.online
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
        v-if="!realtime.online"
        class="connectivity-banner"
        role="status"
        aria-live="polite"
      >
        {{ $t("app.offlineNotice") }}
      </div>
      <div
        v-else-if="!realtime.connected || realtime.replacing"
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
      <main class="content"><RouterView /></main>
    </div>
  </div>
</template>
