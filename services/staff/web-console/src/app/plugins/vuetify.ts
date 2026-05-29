import '@mdi/font/css/materialdesignicons.css';
import 'vuetify/styles';

import { createVuetify } from 'vuetify';

export const vuetify = createVuetify({
  icons: {
    defaultSet: 'mdi',
  },
  theme: {
    defaultTheme: 'kodexLight',
    themes: {
      kodexLight: {
        dark: false,
        colors: {
          background: '#f7f8fb',
          surface: '#ffffff',
          primary: '#ff5a14',
          secondary: '#2563eb',
          success: '#0f9f6e',
          warning: '#d97706',
          error: '#dc2626',
          info: '#2563eb',
        },
      },
    },
  },
  defaults: {
    VBtn: {
      rounded: 'lg',
    },
    VCard: {
      rounded: 'lg',
      elevation: 0,
    },
    VTextField: {
      density: 'compact',
      variant: 'outlined',
      hideDetails: 'auto',
    },
    VSelect: {
      density: 'compact',
      variant: 'outlined',
      hideDetails: 'auto',
    },
    VTextarea: {
      density: 'compact',
      variant: 'outlined',
      hideDetails: 'auto',
    },
  },
});
