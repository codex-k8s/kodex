// Обезличенный вывод настоящего input helper для Go caster/materialization.
import { rootCertificates } from 'node:tls';
import { prepareEmailInput } from './prepare-email-acceptance-input.mjs';
const protocol = process.argv[2] ?? 'IMAP';
const env = { KODEX_QA_EMAIL_ADDRESS: 'sender@example.invalid', KODEX_QA_EMAIL_RECIPIENT: 'recipient@example.invalid', KODEX_QA_EMAIL_CA_PEM_PATH: 'unused' };
for (const kind of ['SMTP', 'IMAP', 'POP3']) for (const [key, value] of Object.entries({ HOST: `${kind.toLowerCase()}.example.invalid`, PORT: { SMTP: '465', IMAP: '993', POP3: '995' }[kind], TLS_MODE: 'implicit', USERNAME: 'fixture', PASSWORD: 'fixture' })) env[`KODEX_QA_EMAIL_${kind}_${key}`] = value;
const specification = prepareEmailInput(env, protocol, 'mvp1321-fixture', () => rootCertificates[0]).profile.specification;
// Proto JSON имеет полные имена enum; сами значения берутся из public helper.
specification.receiveProtocol = `EMAIL_MAILBOX_RECEIVE_PROTOCOL_${specification.receiveProtocol}`;
for (const kind of ['smtp', protocol === 'IMAP' ? 'imap' : 'pop']) {
  const e = specification[kind]; e.tlsMode = `EMAIL_MAILBOX_TLS_MODE_${e.tlsMode}`; e.authMethod = `EMAIL_MAILBOX_AUTH_METHOD_${e.authMethod}`;
  for (const field of ['ca', 'username', 'secret']) e[field] = { name: `email-${kind}-${field}`, generation: 1 };
}
for (const p of specification.policies) { p.operation = `EMAIL_OPERATION_${p.operation}`; p.policy = `EMAIL_APPROVAL_POLICY_${p.policy}`; }
process.stdout.write(JSON.stringify(specification));
