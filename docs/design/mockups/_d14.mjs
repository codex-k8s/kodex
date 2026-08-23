import * as L from './_lib.mjs';
const T = L.T;

const th = (t, w) => `<span style="flex: ${w}; font-size: 11px; letter-spacing: 0.04em; text-transform: uppercase; color: ${T.mut};">${t}</span>`;
const row = (name, sched, tz, target, pill, next, last, actions, sel) => `
  <div style="display: flex; align-items: center; gap: 14px; padding: 13px 16px; border-top: 1px solid ${T.row}; ${sel ? `background: ${T.accTint}; box-shadow: inset 2px 0 0 ${T.acc};` : ''}">
    <span style="flex: 1 1 0; min-width: 0;"><span style="display: block; font-size: 13.5px; font-weight: ${sel ? 600 : 500};">${name}</span></span>
    <span style="flex: 0 0 176px;"><span style="display: block; font-size: 12.5px; color: ${T.ink2};">${sched}</span><span style="display: block; font-size: 11px; color: ${T.mut}; margin-top: 2px;">${tz}</span></span>
    <span style="flex: 0 0 200px; font-size: 12.5px; color: ${T.sec};">${target}</span>
    <span style="flex: 0 0 150px;">${pill}</span>
    <span style="flex: 0 0 176px; font-size: 12.5px; color: ${T.ink2};">${next}</span>
    <span style="flex: 0 0 118px;">${last}</span>
    <span style="flex: 0 0 96px; display: flex; justify-content: flex-end;">${actions}</span>
  </div>`;

// ============ UX-14 Автоматизации ============
export const d14 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('automations')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'Автоматизации'],
    title: 'Автоматизации',
    sub: 'Запускайте регулярную работу по понятному расписанию',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Создать расписание</button>`,
  })}

  <div style="flex: 1 1 auto; display: flex; gap: 18px; padding: 16px 24px 18px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden; display: flex; flex-direction: column; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 14px; padding: 11px 16px; background: ${T.field};">
        ${th('Название', '1 1 0')}${th('Расписание', '0 0 176px')}${th('Цель', '0 0 200px')}${th('Состояние', '0 0 150px')}${th('Следующий запуск', '0 0 176px')}${th('Последний результат', '0 0 118px')}${th('', '0 0 96px')}
      </div>
      ${row('Ежедневная сводка по лидам', 'По будням в 09:00', 'Europe/Saratov', 'ИИ-сотрудник · Аналитик продаж',
        L.statusPill('done', 'Активно', 'sm'), 'Пн, 24 августа, 09:00', L.statusPill('done', 'Успешно', 'sm'), L.btn('Открыть', 'sec', 28), true)}
      ${row('Недельное предложение руководителю', 'Каждую пятницу в 16:00', 'Europe/Saratov', 'Процесс · Подготовка сводки',
        L.statusPill('done', 'Активно', 'sm'), 'Пт, 28 августа, 16:00', L.statusPill('done', 'Успешно', 'sm'), L.btn('Открыть', 'sec', 28), false)}
      ${row('Проверка новых договоров', 'Ежедневно в 07:30', 'Europe/Saratov', 'ИИ-сотрудник · Юридический консультант',
        L.statusPill('off', 'Приостановлено', 'sm'), '—', L.statusPill('done', 'Успешно', 'sm'), L.btn('Открыть', 'sec', 28), false)}
      <div style="flex: 1 1 auto;"></div>
      <div style="padding: 12px 16px; border-top: 1px solid ${T.row}; background: ${T.subtle}; display: flex; align-items: flex-start; gap: 10px;">
        <span style="color: ${T.warn}; display: flex; padding-top: 1px;">${L.icon('alert', 15)}</span>
        <span style="font-size: 12px; color: ${T.sec}; line-height: 1.5;">Вчера уведомление о запуске «Ежедневная сводка по лидам» не было доставлено в Mattermost. Сам запуск выполнен успешно, доставка повторяется отдельно.</span>
      </div>
    </div>

    <aside style="flex: 0 0 396px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; display: flex; flex-direction: column; overflow: hidden;">
      <div style="padding: 14px 16px; border-bottom: 1px solid ${T.line};">
        <div style="font-size: 14.5px; font-weight: 600;">Ежедневная сводка по лидам</div>
        <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 3px;">Настройка расписания</div>
      </div>
      <div style="flex: 1 1 auto; padding: 13px 16px; display: flex; flex-direction: column; gap: 10px; min-height: 0;">        ${L.field('Что запускать', 'ИИ-сотрудник')}
        ${L.field('Цель', 'Аналитик продаж')}
        <div style="display: flex; gap: 10px;">
          ${L.field('Расписание', 'По будням', '', '1 1 0')}
          ${L.field('Время', '09:00', '', '0 0 108px')}
        </div>
        ${L.field('Часовой пояс', 'Europe/Saratov')}
        ${L.textarea("Задание", "Подготовь сводку по новым лидам за предыдущий рабочий день", 46)}
        ${L.field('Сессия', 'Новая сессия для каждого запуска')}
        ${L.field('Уведомления', 'Только Control Center')}
        <div style="display: flex; align-items: center; gap: 10px; padding: 11px 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.subtle};">
          <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 15)}</span>
          <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.sec};">Дополнительные настройки</span>
          <span style="font-size: 11px; color: ${T.mut};">свёрнуто</span>
        </div>
      </div>
      <div style="padding: 12px 16px 16px; border-top: 1px solid ${T.line}; display: flex; gap: 8px;">
        <button style="flex: 1 1 auto; height: 36px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13px; font-weight: 600;">Сохранить расписание</button>
        <button style="flex: 0 0 104px; height: 36px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 12.5px;">Отменить</button>
      </div>
    </aside>
  </div>`,
}));
