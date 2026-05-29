import { createI18n } from 'vue-i18n';

import { ru } from '@/i18n/ru';
import { defaultLocale, setActiveLocale, type AppLocale } from '@/shared/lib/locale';

const requestedLocale = import.meta.env.VITE_KODEX_LOCALE ?? navigator.language;
const initialLocale = setActiveLocale(requestedLocale);

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: defaultLocale,
  messages: {
    ru,
  },
});

export function setApplicationLocale(locale: string): AppLocale {
  const nextLocale = setActiveLocale(locale);
  i18n.global.locale.value = nextLocale;
  return nextLocale;
}
