import { format, formatDistanceToNowStrict, parseISO } from 'date-fns';

import { getDateFnsLocale, getDateTimePattern, getDurationLabels } from '@/shared/lib/locale';

export function formatDateTime(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = parseISO(value);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  return format(date, getDateTimePattern(), { locale: getDateFnsLocale() });
}

export function formatRelativeTime(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = parseISO(value);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  return formatDistanceToNowStrict(date, { addSuffix: true, locale: getDateFnsLocale() });
}

export function formatDurationMs(value?: number): string {
  if (value === undefined || value < 0) {
    return '—';
  }
  const labels = getDurationLabels();
  if (value < 1000) {
    return `${value} ${labels.millisecond}`;
  }
  const seconds = Math.round(value / 1000);
  if (seconds < 60) {
    return `${seconds} ${labels.second}`;
  }
  const minutes = Math.floor(seconds / 60);
  const restSeconds = seconds % 60;
  return restSeconds > 0
    ? `${minutes} ${labels.minute} ${restSeconds} ${labels.second}`
    : `${minutes} ${labels.minute}`;
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
