// Только синтетический owner transport. Сетевые vendor operations не выполняются.
export const emailFixtureProfile = {
  prefix: 'mvp1031-email-fixture',
  specification: {
    enabled: true, receiveProtocol: 'IMAP', sender: 'sender@example.invalid', recipients: ['recipient@example.invalid'], allowedFolders: ['INBOX'], folder: 'INBOX', helloName: 'fixture.invalid',
    smtp: { host: 'smtp.example.invalid', serverName: 'smtp.example.invalid', port: 465, tlsMode: 'IMPLICIT', authMethod: 'PASSWORD' },
    imap: { host: 'imap.example.invalid', serverName: 'imap.example.invalid', port: 993, tlsMode: 'IMPLICIT', authMethod: 'PASSWORD' },
    limits: { attachmentBytes: 1024, maxAttachments: 1, maxRecipients: 1, messageBytes: 4096, pageSize: 1, scanMessages: 1, timeoutSeconds: 5 },
    policies: ['HEALTH', 'MAILBOXES', 'LIST', 'SEARCH', 'FETCH', 'DOWNLOAD', 'SEND', 'REPLY', 'REPLY_ALL', 'FORWARD', 'DELETE', 'RECEIPT', 'THREAD', 'ATTACHMENTS', 'MARK_READ', 'MARK_UNREAD', 'MOVE', 'ARCHIVE', 'DRAFT_CREATE', 'DRAFT_UPDATE', 'DRAFT_DELETE'].map((operation) => ({ operation, policy: operation === 'HEALTH' ? 'ALLOW' : operation === 'SEND' ? 'HUMAN_GATE' : 'DENY', folders: ['INBOX'] })),
  },
};
export function emailFixtureTransport(state, path, method, body, mode = '') {
  const hash = 'a'.repeat(64);
  const connection = () => ({ ref: 'iconn_email', version: state.version ?? 1, definitionKey: 'email', state: state.testing ? 'TESTING' : state.healthy ? 'CONNECTED' : state.healthFailed ? 'DEGRADED' : 'NOT_CONNECTED', ...(state.lastTestedAt ? { lastTestedAt: state.lastTestedAt, [mode === 'summary-field-only' ? 'lastTestSummary' : 'lastTestOutcome']: state.healthFailed ? 'INTEGRATION_CREDENTIAL_UNAVAILABLE' : '' } : {}), grants: state.granted ? [{ ref: 'igrant_email', version: 1, capabilityKey: 'email.message.send', agentRef: 'agt_email', enabled: true }] : [] });
  const view = () => ({ connectionRef: 'iconn_email', connectionVersion: state.version ?? 1, mailboxRef: 'mailbox_email', configuration: { ref: 'mcfg_email', kind: 'EMAIL_MAILBOX', managedBy: 'UI', version: state.configVersion ?? 1 }, revision: { ref: state.revisionRef ?? 'mrev_email', ...(state.parentRef ? { parentRevisionRef: state.parentRef } : {}), state: state.revisionState ?? 'DRAFT', digest: mode === 'drift' ? 'c'.repeat(64) : state.revisionDigest ?? hash }, ...(state.bound ? { publication: { ref: 'empub_email', revision: 2, digest: hash, configurationRevisionRef: state.revisionRef ?? 'mrev_email', state: mode === 'pending' ? 'PENDING' : mode === 'publication-failed' ? 'FAILED' : 'READY' } } : {}) });
  if (path.startsWith('/api/v1/integration-connections?') && method === 'GET') return { body: { items: state.version ? [{ ...connection(), name: 'mvp1031-email-fixture' }] : [], nextPageToken: '' } };
  if (path.includes('/email-mailbox/credential-receipt?') && method === 'GET') return { body: state.credentials?.[new URL(path, 'https://fixture.invalid').searchParams.get('idempotencyKey')] };
  if (path === '/api/v1/integration-connections' && method === 'POST') { state.version = 1; return { status: 201, body: connection() }; }
  if (path === '/api/v1/integration-connections/iconn_email' && method === 'GET') { if (state.testing) { state.testing = false; state.version++; state.lastTestedAt = new Date().toISOString(); } return { body: connection() }; }
  if (path.endsWith('/credential') && method === 'PUT') { state.version++; return { body: { connectionRef: 'iconn_email', connectionVersion: state.version, name: `email-fixture-${state.version}`, generation: 1, kind: body.kind } }; }
  if (path.endsWith('/drafts') && method === 'POST') return { status: 201, body: view() };
  if (path.includes('/email-mailbox/configuration?') && method === 'GET') return { body: view() };
  if (path.endsWith('/saves') && method === 'POST') { state.parentRef = 'mrev_email'; state.revisionRef = 'mrev_email_2'; state.revisionDigest = 'b'.repeat(64); state.revisionState = 'DRAFT'; state.configVersion++; return { body: view() }; }
  if (path.endsWith('/validation') && method === 'POST') { state.revisionState = mode === 'invalid' ? 'INVALID' : 'VALID'; state.configVersion = (state.configVersion ?? 1) + 1; return { body: view() }; }
  if (path.endsWith('/publication') && method === 'POST') { state.revisionState = 'PUBLISHED'; state.configVersion = (state.configVersion ?? 1) + 1; return { body: view() }; }
  if (path.endsWith('/binding') && method === 'POST') { state.bound = true; state.version++; state.configVersion++; return { body: view() }; }
  if (path.endsWith('/commands') && method === 'POST') { state.version++; state.testing = true; state.healthFailed = mode === 'health-failed'; state.healthy = !state.healthFailed; return { body: connection() }; }
  if (path === '/api/v1/agents/agt_email' && method === 'GET') return { body: { ref: 'agt_email', projectRef: mode === 'foreign-agent' ? 'prj_other' : 'prj_email' } };
  if (path.endsWith('/grants') && method === 'POST') { state.version++; state.granted = true; return { body: connection() }; }
  if (path.endsWith('/email-effect-receipt') && method === 'GET') return { body: { receipt: { ref: 'erec_email', version: 1, invocationRef: 'inv_email', connectionRef: mode === 'foreign-receipt' ? 'iconn_other' : 'iconn_email', mailboxRef: 'mailbox_email', projectRef: 'prj_email', configurationRevision: 2, outcome: mode === 'unknown-receipt' ? 'UNKNOWN_OUTCOME' : 'EFFECT_CONFIRMED', externalReceiptDigest: hash, semanticInputDigest: hash } } };
  throw new Error('SYNTHETIC_ROUTE_UNEXPECTED');
}
