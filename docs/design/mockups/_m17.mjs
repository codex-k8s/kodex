import * as L from './_lib.mjs';
const T = L.T;

const memberCard = (init, name, platform, projectRole, perms, pill, note) => L.mCard(`
  <div style="display: flex; align-items: flex-start; gap: 12px;">
    ${L.avatar(init, 34)}
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 15px; font-weight: 600;">${name}</span>
      <span style="display: block; font-size: 12px; color: ${T.mut}; margin-top: 3px;">${platform} · ${projectRole}</span>
    </span>
    <button aria-label="Действия участника" style="width: 44px; height: 44px; margin: -12px -12px 0 0; border: 0; background: none; color: ${T.mut}; display: flex; align-items: center; justify-content: center;">${L.icon('dots', 18)}</button>
  </div>
  <div style="font-size: 12px; color: ${T.sec}; margin-top: 8px; line-height: 1.4;">${perms}</div>
  <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 9px; padding-top: 9px; border-top: 1px solid ${T.row};">
    ${pill}${note ? `<span style="font-size: 11.5px; color: ${T.warn};">${note}</span>` : ''}
  </div>`);

// ============ UX-17 mobile ============
export const m17 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Участники и доступ', back: true, sub: 'Корпоративные продажи' })}
  <div style="flex: 0 0 auto; padding: 10px 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <div style="display: flex; padding: 4px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line};">
      <span style="flex: 1 1 0; height: 38px; display: flex; align-items: center; justify-content: center; border-radius: 8px; font-size: 13px; color: ${T.sec};">Организация</span>
      <span style="flex: 1 1 0; height: 38px; display: flex; align-items: center; justify-content: center; border-radius: 8px; background: ${T.bg}; font-size: 13px; font-weight: 600; box-shadow: 0 1px 2px rgba(16,22,30,0.08);">Проект</span>
    </div>
  </div>
  <div style="flex: 1 1 auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 9px; background: ${T.subtle}; overflow: hidden;">
    ${L.mBtn("Добавить участника", "pri")}
    ${memberCard('АВ', 'Анна Волкова', 'Владелец', 'Владелец Проекта',
      'Полное управление Проектом, участниками и настройками', L.statusPill('done', 'Активна', 'sm'), 'последний владелец')}
    ${memberCard('МО', 'Михаил Орлов', 'Оператор', 'Оператор',
      'Может запускать работу, принимать решения и управлять ИИ-сотрудниками', L.statusPill('done', 'Активен', 'sm'), '')}
    ${memberCard('ЕК', 'Елена Крылова', 'Участник', 'Участник',
      'Может запускать работу и просматривать результаты', L.statusPill('done', 'Активна', 'sm'), '')}
    ${memberCard('ИЛ', 'Игорь Лебедев', 'Аудитор', 'Аудитор',
      'Только просмотр аудита и результатов, без изменений', L.statusPill('done', 'Активен', 'sm'), '')}
  </div>
`));

// ============ UX-18 mobile ============
const statusRow = (title, state, note) => `
  <div style="display: flex; align-items: center; gap: 12px; padding: 13px 14px; border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft};">
    <span style="color: ${T.ok}; display: flex;">${L.icon('check', 18)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 13.5px; font-weight: 600;">${title}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${note}</span>
    </span>
    <span style="font-size: 12.5px; color: ${T.ok}; font-weight: 500; white-space: nowrap;">${state}</span>
  </div>`;

const kv = (k, v) => `<div style="display: flex; gap: 12px; padding: 9px 0; border-top: 1px solid ${T.row};"><span style="flex: 0 0 148px; font-size: 12px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${v}</span></div>`;

export const m18 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Администрирование', back: true, sub: 'Состояние платформы' })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 10px; background: ${T.subtle}; overflow: hidden;">
    <div style="border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 14px; display: flex; align-items: center; gap: 12px;">
      <span style="width: 34px; height: 34px; flex: 0 0 34px; border-radius: 10px; background: ${T.okTint}; border: 1px solid ${T.okLine}; color: ${T.ok}; display: flex; align-items: center; justify-content: center;">${L.icon('check', 19)}</span>
      <span>
        <span style="display: block; font-size: 15.5px; font-weight: 600;">Платформа готова к работе</span>
        <span style="display: block; font-size: 12px; color: ${T.sec}; margin-top: 3px;">Профиль: Web-only</span>
      </span>
    </div>

    ${statusRow('Основные сервисы', 'Готовы', 'Все рабочие пути проверены')}
    ${statusRow('Помощник MatterCodex', 'Готов', 'Горячий runtime доступен')}
    ${statusRow('AI runtime', 'Доступен', 'Стандартный рабочий runtime')}
    ${statusRow('Хранилище', 'Доступно', 'Локальное хранилище платформы')}

    <div style="display: flex; align-items: center; gap: 10px; padding: 12px 14px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg};">
      <span style="flex: 1 1 auto; font-size: 13.5px;">Mattermost</span>
      ${L.statusPill('off', 'Отключён · необязательно', 'sm')}
    </div>

    ${L.mCard(`
      <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px;">
        <span style="font-size: 11.5px; font-weight: 600; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.sec};">Помощник MatterCodex</span>
        <span style="display: inline-flex; align-items: center; height: 22px; padding: 0 9px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11px; color: ${T.sec};">Системный</span>
      </div>
      <div style="margin-top: 6px;">
        ${kv('Основные инструкции', 'v1.3 · защищены')}
        ${kv('Дополнение владельца', 'v2 · можно изменить')}
        ${kv('Горячая сессия', 'Готова')}      </div>
      <div style="display: flex; flex-direction: column; gap: 8px; margin-top: 12px;">
        ${L.mBtn('Открыть помощника', 'sec')}
        ${L.mBtn('Изменить дополнение', 'sec')}
      </div>`)}
  </div>
`));
