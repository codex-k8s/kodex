<script setup lang="ts">
import { ChevronDown, Languages, LogOut } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type {
  BootstrapState,
  UserSummary,
} from "@/shared/api/generated/openapi/types.gen";
import type { SupportedLocale } from "@/shared/locale";

const props = defineProps<{
  user: UserSummary;
  platformRole: BootstrapState["platformRole"];
  locale?: SupportedLocale;
  canLogout?: boolean;
}>();
const emit = defineEmits<{
  changeLocale: [locale: SupportedLocale];
  logout: [];
}>();
const { t } = useI18n();
const roleLabel = computed(() => t(`access.roles.${props.platformRole}`));
const initials = computed(() => {
  const parts = props.user.displayName.trim().split(/\s+/).filter(Boolean);
  return parts
    .slice(0, 2)
    .map((part) => part[0]?.toLocaleUpperCase() ?? "")
    .join("");
});
const accessibleLabel = computed(() =>
  t("app.currentUser", {
    name: props.user.displayName,
    role: roleLabel.value,
  }),
);
</script>

<template>
  <details class="current-user-menu">
    <summary :aria-label="accessibleLabel" :title="accessibleLabel">
      <span class="current-user__avatar" aria-hidden="true">{{
        initials
      }}</span>
      <span class="current-user__text desktop-only">
        <strong>{{ user.displayName }}</strong>
        <small>{{ roleLabel }}</small>
      </span>
      <ChevronDown class="desktop-only" :size="15" aria-hidden="true" />
    </summary>
    <div class="current-user-menu__popover">
      <div class="current-user-menu__identity">
        <strong>{{ user.displayName }}</strong>
        <small>{{ roleLabel }}</small>
      </div>
      <fieldset v-if="locale" class="current-user-menu__languages">
        <legend>
          <Languages :size="16" aria-hidden="true" />
          {{ $t("common.language") }}
        </legend>
        <div class="segmented-control">
          <button
            v-for="value in ['ru', 'en'] as const"
            :key="value"
            type="button"
            :class="{ 'segmented-control__active': locale === value }"
            :aria-pressed="locale === value"
            @click="emit('changeLocale', value)"
          >
            {{ value === "ru" ? "RU" : "EN" }}
          </button>
        </div>
      </fieldset>
      <button
        v-if="canLogout"
        class="current-user-menu__action"
        type="button"
        @click="emit('logout')"
      >
        <LogOut :size="16" aria-hidden="true" />
        {{ $t("auth.logout") }}
      </button>
    </div>
  </details>
</template>

<style scoped>
.current-user-menu {
  position: relative;
  min-width: 0;
}
.current-user-menu summary {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 8px;
  padding: 4px;
  border-radius: 7px;
  cursor: pointer;
  list-style: none;
}
.current-user-menu summary::-webkit-details-marker {
  display: none;
}
.current-user-menu summary:hover,
.current-user-menu[open] summary {
  background: var(--panel);
}
.current-user__avatar {
  display: inline-grid;
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  place-items: center;
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  background: var(--accent-strong);
  border-radius: 50%;
}
.current-user__text {
  max-width: 180px;
}
.current-user__text strong,
.current-user__text small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.current-user__text small {
  color: var(--muted);
}
.current-user-menu__popover {
  position: absolute;
  z-index: 60;
  top: calc(100% + 9px);
  right: 0;
  width: 250px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--surface);
  box-shadow: 0 14px 40px rgba(16, 22, 30, 0.17);
}
.current-user-menu__identity {
  display: grid;
  gap: 2px;
  padding: 5px 6px 10px;
  border-bottom: 1px solid var(--border);
}
.current-user-menu__identity small {
  color: var(--muted);
}
.current-user-menu__languages {
  padding: 10px 6px;
  margin: 0;
  border: 0;
}
.current-user-menu__languages legend {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0;
  color: var(--muted);
  font-size: 12px;
}
.segmented-control {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  margin-top: 7px;
  padding: 2px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.segmented-control button {
  min-height: 28px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  cursor: pointer;
}
.segmented-control__active {
  color: var(--accent-strong);
  background: var(--surface) !important;
  box-shadow: 0 1px 3px rgba(16, 22, 30, 0.12);
}
.current-user-menu__action {
  display: flex;
  align-items: center;
  width: 100%;
  min-height: 34px;
  gap: 8px;
  padding: 6px;
  border: 0;
  border-top: 1px solid var(--border);
  color: var(--text);
  background: transparent;
  cursor: pointer;
  text-align: left;
}
.current-user-menu__action:hover {
  color: var(--accent-strong);
}
</style>
