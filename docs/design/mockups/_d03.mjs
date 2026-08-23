import * as L from './_lib.mjs';
const T = L.T;

const runRow = (title, project, pill, agent, phrase, dur) => `
  <div style="display: flex; align-items: center; gap: 14px; padding: 13px 16px; border-top: 1px solid ${T.row};">
    <div style="flex: 1 1 auto; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 10px;">
        <span style="font-size: 13.5px; font-weight: 600;">${title}</span>
        ${pill}
      </div>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px; font-size: 11.5px; color: ${T.mut};">
        <span>${project}</span>${agent ? `<span>·</span><span>ИИ-сотрудник: <span style="color: ${T.sec};">${agent}</span></span>` : ''}
      </div>
      ${phrase ? `<div style="font-size: 12px; color: ${T.ink2}; margin-top: 5px;">${phrase}</div>` : ''}
    </div>
    <span style="flex: 0 0 auto; font-family: ${T.mono}; font-size: 11.5px; color: ${T.mut};">${dur}</span>
    ${L.btn('Открыть', 'sec', 30)}
  </div>`;

const resultRow = (name, project, time) => `
  <div style="display: flex; align-items: center; gap: 12px; padding: 11px 16px; border-top: 1px solid ${T.row};">
    <span style="color: ${T.sec}; display: flex;">${L.icon('file', 17)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 13px; font-weight: 500;">${name}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${project} · ${time}</span>
    </span>
    ${L.btn('Открыть', 'sec', 28)}
  </div>`;

// ============ UX-03 Главная ============
export const d03 = L.page(L.shellDesktop({
  nav: 'home', project: 'Все проекты',
  body: `
  ${L.contentHead({
    path: ['Главная'],
    title: 'Добрый день, Анна',
    sub: 'Вот что происходит с вашими ИИ-сотрудниками',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Запустить работу</button>`,
  })}

  <div style="flex: 1 1 auto; padding: 20px 24px; display: flex; flex-direction: column; gap: 18px; background: ${T.subtle}; min-height: 0;">

    <div style="display: flex; gap: 14px;">
      ${L.metric('3', 'проекта')}
      ${L.metric('7', 'ИИ-сотрудников')}
      ${L.metric('2', 'активных запуска')}
      ${L.metric('1', 'решение ожидает')}
    </div>

    <div style="flex: 1 1 auto; display: flex; gap: 18px; min-height: 0;">

      <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 18px; min-width: 0;">
        <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
          <div style="display: flex; align-items: center; justify-content: space-between; padding: 13px 16px;">
            <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Текущая работа</h2>
            <a href="#" style="font-size: 12px;">Все запуски</a>
          </div>
          ${runRow('Подготовить коммерческое предложение', 'Корпоративные продажи', L.statusPill('run', 'Работает', 'sm'), 'Аналитик продаж', 'Сравниваю требования клиента с загруженными материалами', '12 мин')}
          ${runRow('Разобрать обращения за неделю', 'Клиентская поддержка', L.statusPill('wait', 'В очереди', 'sm'), '', '', '—')}
        </div>

        <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
          <div style="display: flex; align-items: center; justify-content: space-between; padding: 13px 16px;">
            <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Последние результаты</h2>
            <a href="#" style="font-size: 12px;">Все файлы</a>
          </div>
          ${resultRow('Отчёт по обращениям.pdf', 'Клиентская поддержка', 'вчера, 18:20')}
          ${resultRow('Рекомендации по лидам.docx', 'Корпоративные продажи', 'вчера, 12:05')}
        </div>
      </div>

      <div style="flex: 0 0 380px; display: flex; flex-direction: column; gap: 18px;">
        <div style="border: 1px solid ${T.warnLine}; border-radius: 12px; background: ${T.warnTint}; padding: 16px;">
          <div style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.warn};">
            ${L.icon('shield', 15)}Требует решения
          </div>
          <div style="font-size: 14.5px; font-weight: 600; margin-top: 12px;">Согласовать коммерческое предложение</div>
          <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 5px;">Процесс ждёт вашего решения</div>
          <div style="display: flex; align-items: center; gap: 7px; margin-top: 10px; font-size: 12px; color: ${T.warn};">
            ${L.icon('clock', 14)}Сегодня, 18:00
          </div>
          <button style="width: 100%; margin-top: 14px; height: 36px; border-radius: 9px; border: 0; background: ${T.warn}; color: #FFFFFF; font: inherit; font-size: 13px; font-weight: 600;">Рассмотреть</button>
        </div>

        <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <h2 style="margin: 0 0 12px; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Быстрый старт</h2>
          <div style="display: flex; flex-direction: column; gap: 8px;">
            <a href="#" style="display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 9px; border: 1px solid ${T.line}; color: ${T.ink}; font-size: 13px;"><span style="color: ${T.acc}; display: flex;">${L.icon('bot', 16)}</span>Создать ИИ-сотрудника<span style="margin-left: auto; color: ${T.mut}; display: flex;">${L.icon('chevR', 14)}</span></a>
            <a href="#" style="display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 9px; border: 1px solid ${T.line}; color: ${T.ink}; font-size: 13px;"><span style="color: ${T.acc}; display: flex;">${L.icon('wf', 16)}</span>Создать Процесс<span style="margin-left: auto; color: ${T.mut}; display: flex;">${L.icon('chevR', 14)}</span></a>
            <a href="#" style="display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 9px; border: 1px solid ${T.line}; color: ${T.ink}; font-size: 13px;"><span style="color: ${T.acc}; display: flex;">${L.icon('upload', 16)}</span>Загрузить файл<span style="margin-left: auto; color: ${T.mut}; display: flex;">${L.icon('chevR', 14)}</span></a>
          </div>
        </div>

        <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 16px; display: flex; align-items: flex-start; gap: 10px;">
          <span style="color: ${T.mut}; display: flex; padding-top: 1px;">${L.icon('alert', 15)}</span>
          <span style="font-size: 12px; color: ${T.sec}; line-height: 1.5;">Доставка в Mattermost не выполнена для одного уведомления. На результат запусков это не влияет.</span>
        </div>
      </div>
    </div>
  </div>`,
}));
