import * as L from './_lib.mjs';
const T = L.T;

const tabs = (items, active) => `
  <div style="display: flex; gap: 4px; padding: 0 24px; border-bottom: 1px solid ${T.line}; background: ${T.bg}; flex: 0 0 42px; align-items: stretch;">
    ${items.map((t) => `<span style="display: flex; align-items: center; padding: 0 12px; font-size: 12.5px; ${t === active ? `color: ${T.ink}; font-weight: 600; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${t}</span>`).join('')}
  </div>`;

const statusCard = (title, state, note) => `
  <div style="flex: 1 1 0; border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 14px 16px;">
    <div style="font-size: 12px; color: ${T.sec};">${title}</div>
    <div style="display: flex; align-items: center; gap: 8px; margin-top: 8px; font-size: 14px; font-weight: 600; color: ${T.ok};">${L.icon('check', 17)}${state}</div>
    <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 6px;">${note}</div>
  </div>`;

const kv = (k, v) => `<div style="display: flex; gap: 12px; padding: 9px 0; border-top: 1px solid ${T.row};"><span style="flex: 0 0 190px; font-size: 12px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${v}</span></div>`;

const adapterRow = (name, st) => `
  <div style="display: flex; align-items: center; gap: 12px; padding: 10px 0; border-top: 1px solid ${T.row};">
    <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${name}</span>${st}
  </div>`;

// ============ UX-18 Администрирование ============
export const d18 = L.page(L.shellDesktop({
  nav: 'admin', project: 'Все проекты',
  body: `
  ${L.contentHead({
    path: ['Администрирование'],
    title: 'Администрирование',
    sub: 'Состояние платформы и системные настройки',
  })}
  ${tabs(['Организация', 'AI runtimes', 'Помощник Kodex', 'Хранилище и резервирование', 'Необязательные адаптеры'], 'Организация')}

  <div style="flex: 1 1 auto; padding: 18px 24px 22px; display: flex; flex-direction: column; gap: 16px; background: ${T.subtle}; min-height: 0;">

    <div style="display: flex; align-items: center; gap: 14px; padding: 14px 18px; border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft};">
      <span style="width: 34px; height: 34px; border-radius: 10px; background: ${T.okTint}; border: 1px solid ${T.okLine}; color: ${T.ok}; display: flex; align-items: center; justify-content: center;">${L.icon('check', 19)}</span>
      <span style="flex: 1 1 auto;">
        <span style="display: block; font-size: 16px; font-weight: 600;">Платформа готова к работе</span>
        <span style="display: block; font-size: 12.5px; color: ${T.sec}; margin-top: 3px;">Профиль установки: Web-only · Mattermost отключён, на выполнение задач это не влияет</span>
      </span>
      <span style="font-family: ${T.mono}; font-size: 11.5px; color: ${T.mut};">состояние обновляется автоматически</span>
    </div>

    <div style="display: flex; gap: 14px;">
      ${statusCard('Основные сервисы', 'Готовы', 'Все рабочие пути проверены')}
      ${statusCard('Помощник Kodex', 'Горячий runtime готов', 'Системная сессия доступна')}
      ${statusCard('AI runtime', 'Доступен', 'Стандартный рабочий runtime')}
      ${statusCard('Хранилище', 'Доступно', 'Локальное хранилище платформы')}
    </div>

    <div style="flex: 1 1 auto; display: flex; gap: 16px; min-height: 0;">
      <div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px 18px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Организация</div>
        <div style="margin-top: 10px;">
          ${kv('Название', 'Kodex')}
          ${kv('Язык по умолчанию', 'Русский')}
          ${kv('Часовой пояс', 'Europe/Saratov')}
          ${kv('Владелец', 'Анна Волкова · единственный владелец')}
          ${kv('Участники', '4 активных участника')}
        </div>
        <div style="display: flex; gap: 8px; margin-top: 14px;">
          ${L.btn('Изменить параметры', 'sec', 32)}
          ${L.btn('Открыть участников', 'sec', 32)}
        </div>
      </div>

      <div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px 18px;">
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Помощник Kodex</div>
          <span style="display: inline-flex; align-items: center; height: 22px; padding: 0 9px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11px; color: ${T.sec};">Системный · неудаляемый</span>
        </div>
        <div style="margin-top: 10px;">
          ${kv('Основные инструкции', 'v1.3 · защищены')}
          ${kv('Дополнение владельца', 'v2 · можно изменить')}
          ${kv('Горячая сессия', 'Готова')}
          ${kv('Ходы', 'Выполняются последовательно')}
          ${kv('Ограничения', '1 горячий runtime · лимиты CPU и памяти настроены')}
        </div>
        <div style="display: flex; gap: 8px; margin-top: 14px;">
          ${L.btn('Открыть помощника', 'sec', 32)}
          ${L.btn('Изменить дополнение', 'sec', 32)}
          ${L.btn('Посмотреть аудит', 'sec', 32)}
        </div>
      </div>

      <div style="flex: 0 0 356px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px 18px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Необязательные адаптеры · Mattermost</div>
        <div style="margin-top: 8px;">
          ${adapterRow('Входящие сообщения', L.statusPill('off', 'Отключено', 'sm'))}
          ${adapterRow('Уведомления', L.statusPill('off', 'Отключено', 'sm'))}
          ${adapterRow('Зеркало результатов', L.statusPill('off', 'Отключено', 'sm'))}
          ${adapterRow('Решения человека', L.statusPill('off', 'Отключено', 'sm'))}
        </div>
        <div style="margin-top: 14px; padding: 11px 12px; border-radius: 9px; background: ${T.okSoft}; border: 1px solid ${T.okLine}; font-size: 12px; color: ${T.ink2}; line-height: 1.5;">
          Готовность платформы остаётся «Готова»: необязательные адаптеры не входят в основную работоспособность.
        </div>
        <div style="display: flex; align-items: center; gap: 10px; margin-top: 14px; padding: 11px 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.subtle};">
          <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 15)}</span>
          <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.sec};">Технические подробности</span>
          <span style="font-size: 11px; color: ${T.mut};">роль администратора</span>
        </div>
      </div>
    </div>
  </div>`,
}));
