import { createPinia } from 'pinia';
import { createApp } from 'vue';

import App from './App.vue';
import { i18n } from './app/i18n';
import { router } from './app/router';
import { vuetify } from './app/plugins/vuetify';
import './app/styles/main.scss';

const app = createApp(App);

app.use(createPinia());
app.use(router);
app.use(i18n);
app.use(vuetify);

app.mount('#app');
