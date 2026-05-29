import axios, { type AxiosError } from 'axios';

import type { SafeError } from './generated';

export type ApiErrorKind = 'network' | 'timeout' | 'canceled' | 'http';

export type ApiError = {
  kind: ApiErrorKind;
  status?: number;
  code: string;
  messageKey: string;
  requestId?: string;
  correlationId?: string;
  retryable: boolean;
};

const knownErrorCodes = new Set([
  'invalid_request',
  'unauthenticated',
  'permission_denied',
  'not_found',
  'conflict',
  'stale_version',
  'downstream_unavailable',
  'rate_limited',
]);

export function normalizeApiError(error: unknown): ApiError {
  if (axios.isCancel(error)) {
    return {
      kind: 'canceled',
      code: 'canceled',
      messageKey: 'errors.canceled',
      retryable: false,
    };
  }

  if (axios.isAxiosError(error)) {
    return fromAxiosError(error);
  }

  return {
    kind: 'network',
    code: 'unknown',
    messageKey: 'errors.unknown',
    retryable: false,
  };
}

function fromAxiosError(error: AxiosError<SafeError>): ApiError {
  if (error.code === 'ECONNABORTED') {
    return {
      kind: 'timeout',
      code: 'timeout',
      messageKey: 'errors.timeout',
      retryable: true,
    };
  }

  const safe = error.response?.data;
  if (safe?.code) {
    const code = knownErrorCodes.has(safe.code) ? safe.code : 'unknown';
    return {
      kind: 'http',
      status: error.response?.status,
      code,
      messageKey: `errors.${code}`,
      requestId: safe.request_id,
      correlationId: safe.correlation_id,
      retryable: safe.retryable,
    };
  }

  if (error.response) {
    return {
      kind: 'http',
      status: error.response.status,
      code: 'unknown',
      messageKey: 'errors.unknown',
      retryable: error.response.status >= 500,
    };
  }

  return {
    kind: 'network',
    code: 'network',
    messageKey: 'errors.network',
    retryable: true,
  };
}
