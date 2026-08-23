import * as L from './_lib.mjs';
const T = L.T;

const stepCard = (n, title, who, meta, opts = {}) => `
  <div style="border: 1px solid ${opts.warn ? T.warnLine : T.line}; border-radius: 11px; background: ${opts.warn ? T.warnTint : T.bg}; padding: 13px 14px; display: flex; align-items: flex-start; gap: 12px;">
    <span style="width: 22px; height: 22px; flex: 0 0 22px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line}; font-family: ${T.mono}; font-size: 11px; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${n}</span>
    <div style="flex: 1 1 auto; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 9px;">
        <span style="font-size: 13.5px; font-weight: 600;">${title}</span>
        ${who ? `<span style="font-size: 11.5px; color: ${T.mut};">${who}</span>` : ''}
        ${opts.badge || ''}
      </div>
      <div style="font-size: 12px; color: ${T.sec}; margin-top: 4px; line-height: 1.45;">${meta}</div>
      ${opts.err ? `<div style="display: flex; align-items: center; gap: 7px; margin-top: 8px; font-size: 12px; color: ${T.warn};">${L.icon('alert', 14)}${opts.err}</div>` : ''}
    </div>
    <span style="display: flex; align-items: center; gap: 4px; flex: 0 0 auto;">
      <button aria-label="Переместить выше" style="width: 26px; height: 26px; border-radius: 6px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 15 6-6 6 6"/></svg></button>
      <button aria-label="Переместить ниже" style="width: 26px; height: 26px; border-radius: 6px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg></button>
      <button aria-label="Действия шага" style="width: 26px; height: 26px; border-radius: 6px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('dots', 14)}</button>
    </span>
  </div>`;

const formNav = (items, active) => `
  <nav style="width: 196px; flex: 0 0 196px; display: flex; flex-direction: column; gap: 2px; padding-right: 16px; border-right: 1px solid ${T.line};">
    ${items.map((t) => `<span style="padding: 8px 11px; border-radius: 8px; font-size: 12.5px; ${t === active ? `background: ${T.accTint}; color: ${T.accDark}; font-weight: 600;` : `color: ${T.sec};`}">${t}</span>`).join('')}
  </nav>`;

// ============ UX-09 Редактор Процесса ============
export const d09 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('workflows')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'Процессы', 'Обработка нового лида'],
    title: 'Обработка нового лида',
    sub: 'Есть неопубликованные изменения. Запускается последняя опубликованная версия v3.',
    pill: L.statusPill('wait', 'Черновик v4'),
    actions: `${L.btn('Запустить', 'off', 36)}${L.btn('Проверить', 'sec', 36)}<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Опубликовать v4</button>`,
  })}

  <div style="flex: 1 1 auto; display: flex; padding: 18px 24px 22px; gap: 18px; background: ${T.subtle}; min-height: 0;">
    ${formNav(['Основное', 'Входные данные', 'Участники', 'Шаги', 'Решения человека', 'Результат', 'Настройки', 'Версии'], 'Шаги')}

    <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 12px; min-width: 0;">
      <div style="display: flex; align-items: center; justify-content: space-between;">
        <h2 style="margin: 0; font-size: 15px; font-weight: 600;">Шаги</h2>
        ${L.btn('Добавить шаг', 'sec', 32)}
      </div>

      ${stepCard(1, 'Приём входных данных', '· Менеджер продаж', 'Координатор принимает данные лида и ставит задачи. Таймаут шага: 10 мин.')}
      ${stepCard(2, 'Исследовать компанию', '· Аналитик продаж', 'Вход: данные лида и файлы. Результат: подтверждённые факты и риски. Таймаут: 30 мин.')}

      <div style="border: 1px dashed ${T.lineStrong}; border-radius: 12px; background: ${T.bg}; padding: 12px;">
        <div style="font-size: 11.5px; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.mut}; padding: 0 2px 9px;">Шаг 3 · параллельная группа · до 2 исполнений</div>
        <div style="display: flex; flex-direction: column; gap: 10px;">
          ${stepCard('3a', 'Подготовить предложение', '· Редактор предложений', 'Вход: факты исследования. Результат: документ предложения. Таймаут: 45 мин.')}
          ${stepCard('3b', 'Оценить риски', '· Юридический консультант', 'Вход: условия сделки. Таймаут: 30 мин.', { warn: true, err: 'Не задано описание результата — публикация недоступна' })}
        </div>
      </div>

      ${stepCard(4, 'Согласовать предложение', '', 'Решения: «Утвердить», «Запросить изменения», «Отклонить». Ответ вернётся координатору.', { badge: L.statusPill('gate', 'Решение человека', 'sm') })}
      ${stepCard(5, 'Собрать итог', '· Менеджер продаж', 'Координатор формирует итоговый результат запуска.')}

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 16px; display: flex; gap: 28px;">
        <span style="font-size: 12.5px; color: ${T.sec};">Критерий завершения<br><span style="color: ${T.ink};">Предложение подготовлено и решение получено</span></span>
        <span style="font-size: 12.5px; color: ${T.sec};">Таймаут Процесса<br><span style="color: ${T.ink};">2 часа</span></span>
        <span style="font-size: 12.5px; color: ${T.sec};">Параллельность<br><span style="color: ${T.ink};">До 2 параллельных исполнений</span></span>
      </div>
    </div>

    <aside style="flex: 0 0 300px; display: flex; flex-direction: column; gap: 14px;">
      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 16px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Предпросмотр графа</div>
        <div style="margin-top: 12px; display: flex; flex-direction: column; gap: 6px; font-size: 12.5px;">
          <span style="display: flex; align-items: center; gap: 8px;"><span style="width: 8px; height: 8px; border-radius: 4px; background: ${T.lineStrong};"></span>Менеджер продаж</span>
          <span style="display: flex; align-items: center; gap: 8px; padding-left: 16px;"><span style="width: 8px; height: 8px; border-radius: 4px; background: ${T.lineStrong};"></span>Аналитик продаж</span>
          <span style="display: flex; align-items: center; gap: 8px; padding-left: 32px;"><span style="width: 8px; height: 8px; border-radius: 4px; background: ${T.lineStrong};"></span>Редактор предложений</span>
          <span style="display: flex; align-items: center; gap: 8px; padding-left: 32px;"><span style="width: 8px; height: 8px; border-radius: 4px; background: ${T.warn};"></span>Юридический консультант</span>
          <span style="display: flex; align-items: center; gap: 8px; padding-left: 16px;"><span style="width: 8px; height: 8px; border-radius: 4px; background: ${T.warn};"></span>Согласовать предложение</span>
          <span style="display: flex; align-items: center; gap: 8px;"><span style="width: 8px; height: 8px; border-radius: 4px; background: ${T.lineStrong};"></span>Собрать итог</span>
        </div>
        <p style="margin: 12px 0 0; font-size: 11.5px; color: ${T.mut}; line-height: 1.5;">Предпросмотр только показывает структуру и не является источником состояния запуска.</p>
      </div>

      <div style="border: 1px solid ${T.warnLine}; border-radius: 12px; background: ${T.warnTint}; padding: 14px 16px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Проверка</div>
        <div style="display: flex; flex-direction: column; gap: 9px; margin-top: 11px; font-size: 12.5px;">
          <span style="display: flex; align-items: center; gap: 8px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 14)}</span>5 шагов</span>
          <span style="display: flex; align-items: center; gap: 8px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 14)}</span>3 ИИ-сотрудника</span>
          <span style="display: flex; align-items: center; gap: 8px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 14)}</span>1 решение человека</span>
          <span style="display: flex; align-items: center; gap: 8px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 14)}</span>Входные данные корректны</span>
          <span style="display: flex; align-items: flex-start; gap: 8px; color: ${T.warn}; line-height: 1.4;"><span style="display: flex; padding-top: 1px;">${L.icon('alert', 14)}</span>Для шага «Оценить риски» не задано описание результата</span>
        </div>
      </div>
    </aside>
  </div>`,
}));
