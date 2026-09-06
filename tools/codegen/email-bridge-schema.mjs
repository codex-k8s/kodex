import fs from 'node:fs';

const source = JSON.parse(fs.readFileSync('contracts/openapi/email-bridge/v1/openapi.yaml', 'utf8'));
const definitions = JSON.parse(JSON.stringify(source.components.schemas).replaceAll('#/components/schemas/', '#/$defs/'));
const document = { $schema: 'https://json-schema.org/draft/2020-12/schema', $ref: '#/$defs/Configuration', $defs: definitions };
const text = JSON.stringify(document, null, 2) + '\n';
for (const path of ['contracts/email-bridge/v1/configuration.schema.json', 'libs/go/emailbridgeapi/schema.gen.json']) {
  if (process.argv.includes('--check')) {
    if (fs.readFileSync(path, 'utf8') !== text) throw new Error('Email bridge schema codegen mismatch');
  } else {
    fs.mkdirSync(path.slice(0, path.lastIndexOf('/')), { recursive: true });
    fs.writeFileSync(path, text);
  }
}
