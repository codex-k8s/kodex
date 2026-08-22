import * as L from './_lib.mjs';
const T = L.T;

const teamRow = (init, tone, name, role, pill) => `
  <div style="display: flex; align-items: center; gap: 11px; padding: 11px 16px; border-top: 1px solid ${T.row};">
    ${L.avatar(init, 30, tone)}
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 13px; font-weight: 500;">${name}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${role}</span>
    </span>
    ${pill}
  </div>`;

const wfRow = (name, sub, ver) => `
  <div style="display: flex; align-items: center; gap: 12px; padding: 11px 16px; border-top: 1px solid ${T.row};">
    <span style="color: ${T.sec}; display: flex;">${L.icon('wf', 17)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 13px; font-weight: 500;">${name}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${sub}</span>
    </span>
    <span style="font-size: 11.5px; color: ${T.mut};">${ver}</span>
    ${L.btn('Запустить', 'sec', 28)}
  </div>`;

// ============ UX-05 Обзор Проекта ============
export const d05 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('overview')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи'],
    title: 'Корпоративные продажи',
    sub: 'Подготовка предложений и аналитика клиентов',
    pill: L.statusPill('done', 'Активен'),
    actions: `${L.btn('Создать ИИ-сотрудника', 'sec', 36)}<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Запустить работу</button>${L.iconBtn('dots', 'Настройки проекта', 36)}`,
  })}

  <div style="flex: 1 1 auto; padding: 18px 24px 22px; display: flex; flex-direction: column; gap: 16px; background: ${T.subtle}; min-height: 0;">

    <div style="display: flex; gap: 14px;">
      ${L.metric('3', 'ИИ-сотрудника')}
      ${L.metric('2', 'Процесса')}
      ${L.metric('1', 'активный запуск')}
      ${L.metric('1', 'ожидает решения')}
    </div>

    <div style="display: flex; gap: 16px;">
      <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px; min-width: 0;">
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Сейчас выполняется</h2>
          ${L.statusPill('run', 'Выполняется', 'sm')}
        </div>
        <div style="font-size: 15px; font-weight: 600; margin-top: 12px;">Подготовить предложение для компании Север</div>
        <div style="display: flex; align-items: center; gap: 10px; margin-top: 6px; font-size: 12px; color: ${T.mut};">
          <span style="font-family: ${T.mono}; color: ${T.sec};">12 мин</span><span>·</span>
          <span>ИИ-сотрудник: <span style="color: ${T.sec};">Редактор предложений</span></span>
        </div>
        <div style="margin-top: 10px; padding: 10px 12px; border-radius: 9px; background: ${T.accSoft}; border: 1px solid ${T.accSoftLine}; font-size: 12.5px; color: ${T.ink2};">Собираю итоговую структуру документа</div>
        <div style="margin-top: 12px;">${L.btn('Открыть запуск', 'sec', 32)}</div>
      </div>

      <div style="flex: 0 0 380px; border: 1px solid ${T.warnLine}; border-radius: 12px; background: ${T.warnTint}; padding: 16px;">
        <div style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.warn};">${L.icon('shield', 15)}Требует решения</div>
        <div style="font-size: 15px; font-weight: 600; margin-top: 12px;">Согласовать коммерческое предложение</div>
        <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 5px;">Решение человека в запуске «Подготовить предложение для компании Север»</div>
        <button style="width: 100%; margin-top: 14px; height: 34px; border-radius: 9px; border: 0; background: ${T.warn}; color: #FFFFFF; font: inherit; font-size: 13px; font-weight: 600;">Рассмотреть</button>
      </div>
    </div>

    <div style="flex: 1 1 auto; display: flex; gap: 16px; min-height: 0;">
      <div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
        <div style="display: flex; align-items: center; justify-content: space-between; padding: 13px 16px;">
          <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Команда</h2>
          <a href="#" style="font-size: 12px;">Все ИИ-сотрудники</a>
        </div>
        ${teamRow('АП', 'neutral', 'Аналитик продаж', 'Исследование клиентов', L.statusPill('done', 'Готов', 'sm'))}
        ${teamRow('РП', 'acc', 'Редактор предложений', 'Итоговые документы', L.statusPill('run', 'Выполняет', 'sm'))}
        ${teamRow('ЮК', 'neutral', 'Юридический консультант', 'Условия и риски', L.statusPill('off', 'Отключён', 'sm'))}
      </div>

      <div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
        <div style="display: flex; align-items: center; justify-content: space-between; padding: 13px 16px;">
          <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Процессы</h2>
          <a href="#" style="font-size: 12px;">Все Процессы</a>
        </div>
        ${wfRow('Обработка нового лида', '3 ИИ-сотрудника · 1 решение человека', 'v3')}
        ${wfRow('Подготовка предложения', '2 ИИ-сотрудника · без решений', 'v1')}
        <div style="padding: 13px 16px; border-top: 1px solid ${T.row};">
          <h2 style="margin: 0 0 10px; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Последние результаты</h2>
          <div style="display: flex; align-items: center; gap: 11px;">
            <span style="color: ${T.sec}; display: flex;">${L.icon('file', 17)}</span>
            <span style="flex: 1 1 auto;">
              <span style="display: block; font-size: 13px; font-weight: 500;">Коммерческое предложение.pdf</span>
              <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">вчера, 16:40</span>
            </span>
            ${L.btn('Открыть', 'sec', 28)}
          </div>
        </div>
      </div>

      <div style="flex: 0 0 380px; border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 16px;">
        <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Готовность проекта</h2>
        <div style="display: flex; align-items: center; gap: 9px; margin-top: 13px; font-size: 14.5px; font-weight: 600; color: ${T.ok};">${L.icon('check', 18)}Можно запускать работу</div>
        <div style="display: flex; flex-direction: column; gap: 10px; margin-top: 14px;">
          <span style="display: flex; align-items: flex-start; gap: 9px; font-size: 12.5px; color: ${T.ink2}; line-height: 1.45;"><span style="color: ${T.ok}; display: flex; padding-top: 1px;">${L.icon('check', 14)}</span>Команда ИИ-сотрудников создана</span>
          <span style="display: flex; align-items: flex-start; gap: 9px; font-size: 12.5px; color: ${T.ink2}; line-height: 1.45;"><span style="color: ${T.ok}; display: flex; padding-top: 1px;">${L.icon('check', 14)}</span>Есть опубликованные Процессы</span>
          <span style="display: flex; align-items: flex-start; gap: 9px; font-size: 12.5px; color: ${T.sec}; line-height: 1.45;"><span style="color: ${T.mut}; display: flex; padding-top: 1px;">${L.icon('minus', 14)}</span>Интеграции не подключены — это не мешает работе</span>
        </div>
      </div>
    </div>
  </div>`,
}));
