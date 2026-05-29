import { createI18n } from 'vue-i18n';

import { ru } from '@/i18n/ru';

export const defaultLocale = 'ru';

export const i18n = createI18n({
  legacy: false,
  locale: defaultLocale,
  fallbackLocale: defaultLocale,
  messages: {
    ru,
  },
});
