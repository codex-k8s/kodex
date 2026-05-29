import type { ActorTypeHeader, ScopeType } from './generated';
import { createRequestId } from '@/shared/lib/request';

export type OperatorContext = {
  actorType: ActorTypeHeader;
  actorId: string;
  scopeType: ScopeType;
  scopeRef: string;
};

export type GatewayHeaders = {
  'X-Kodex-Request-Id': string;
  'X-Kodex-Actor-Type': ActorTypeHeader;
  'X-Kodex-Actor-Id': string;
  'X-Kodex-Trace-Id'?: string;
  'X-Kodex-Session-Id'?: string;
};

export const actorTypeOptions: ActorTypeHeader[] = ['user', 'service', 'agent', 'external_account'];
export const scopeTypeOptions: ScopeType[] = ['platform', 'organization', 'project', 'repository', 'service'];

export function isOperatorContextReady(context: OperatorContext): boolean {
  return context.actorId.trim().length > 0 && context.scopeRef.trim().length > 0;
}

export function gatewayHeaders(context: OperatorContext): GatewayHeaders {
  return {
    'X-Kodex-Request-Id': createRequestId(),
    'X-Kodex-Actor-Type': context.actorType,
    'X-Kodex-Actor-Id': context.actorId.trim(),
  };
}
