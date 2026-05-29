import { defineConfig } from '@hey-api/openapi-ts';

export default defineConfig({
  input: '../../../specs/openapi/staff-gateway.v1.yaml',
  output: {
    path: 'src/shared/api/generated',
    format: 'prettier',
  },
  plugins: ['@hey-api/client-axios'],
});
