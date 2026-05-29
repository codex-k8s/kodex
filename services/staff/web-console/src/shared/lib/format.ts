import { format, formatDistanceToNowStrict, parseISO } from 'date-fns';
import { ru } from 'date-fns/locale/ru';

export function formatDateTime(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = parseISO(value);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  return format(date, 'dd.MM.yyyy HH:mm', { locale: ru });
}

export function formatRelativeTime(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = parseISO(value);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  return formatDistanceToNowStrict(date, { addSuffix: true, locale: ru });
}

export function formatDurationMs(value?: number): string {
  if (value === undefined || value < 0) {
    return '—';
  }
  if (value < 1000) {
    return `${value} мс`;
  }
  const seconds = Math.round(value / 1000);
  if (seconds < 60) {
    return `${seconds} с`;
  }
  const minutes = Math.floor(seconds / 60);
  const restSeconds = seconds % 60;
  return restSeconds > 0 ? `${minutes} мин ${restSeconds} с` : `${minutes} мин`;
}

export function compactRef(value?: string): string {
  if (!value) {
    return '—';
  }
  if (value.length <= 28) {
    return value;
  }
  return `${value.slice(0, 12)}…${value.slice(-8)}`;
}

export function prettySafeJSON(value?: string): string {
  if (!value) {
    return '';
  }
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}
