import type { Locale } from 'date-fns';
import { ru as dateFnsRu } from 'date-fns/locale/ru';

export const supportedLocales = ['ru'] as const;
export type AppLocale = (typeof supportedLocales)[number];

export const defaultLocale: AppLocale = 'ru';

type DurationLabels = {
  millisecond: string;
  second: string;
  minute: string;
};

type LocaleConfig = {
  acceptLanguage: string;
  dateFnsLocale: Locale;
  dateTimePattern: string;
  duration: DurationLabels;
};

const localeConfigs: Record<AppLocale, LocaleConfig> = {
  ru: {
    acceptLanguage: 'ru',
    dateFnsLocale: dateFnsRu,
    dateTimePattern: 'dd.MM.yyyy HH:mm',
    duration: {
      millisecond: 'мс',
      second: 'с',
      minute: 'мин',
    },
  },
};

let activeLocale: AppLocale = defaultLocale;

export function resolveAppLocale(value?: string): AppLocale {
  const normalized = value?.toLowerCase().split('-')[0];
  if (normalized && supportedLocales.includes(normalized as AppLocale)) {
    return normalized as AppLocale;
  }
  return defaultLocale;
}

export function setActiveLocale(value?: string): AppLocale {
  activeLocale = resolveAppLocale(value);
  return activeLocale;
}

export function getActiveLocale(): AppLocale {
  return activeLocale;
}

export function getAcceptLanguage(): string {
  return localeConfigs[activeLocale].acceptLanguage;
}

export function getDateFnsLocale(): Locale {
  return localeConfigs[activeLocale].dateFnsLocale;
}

export function getDateTimePattern(): string {
  return localeConfigs[activeLocale].dateTimePattern;
}

export function getDurationLabels(): DurationLabels {
  return localeConfigs[activeLocale].duration;
}
