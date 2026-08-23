import * as L from './_lib.mjs';
const T = L.T;

const step = (n, title, who, meta, opts = {}) => `
  <div style="border: 1px solid ${opts.warn ? T.warnLine : T.line}; border-radius: 12px; background: ${opts.warn ? T.warnTint : T.bg}; padding: 10px 11px;">
    <div style="display: flex; align-items: flex-start; gap: 11px;">
      <span style="width: 24px; height: 24px; flex: 0 0 24px; border-radius: 12px; background: ${T.field}; border: 1px solid ${T.line}; font-family: ${T.mono}; font-size: 11px; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${n}</span>
      <span style="flex: 1 1 auto; min-width: 0;">
        <span style="display: block; font-size: 14px; font-weight: 600; line-height: 1.3;">${title}</span>
        ${who ? `<span style="display: block; font-size: 12px; color: ${T.mut}; margin-top: 3px;">${who}</span>` : ''}
        <span style="display: block; font-size: 12px; color: ${T.sec}; margin-top: 5px; line-height: 1.4;">${meta}</span>
        ${opts.err ? `<span style="display: flex; align-items: flex-start; gap: 7px; margin-top: 8px; font-size: 12px; color: ${T.warn}; line-height: 1.4;"><span style="display: flex; padding-top: 1px;">${L.icon('alert', 14)}</span>${opts.err}</span>` : ''}
      </span>
      <button aria-label="Действия шага" style="width: 44px; height: 44px; margin: -10px -10px 0 0; border: 0; background: none; color: ${T.mut}; display: flex; align-items: center; justify-content: center;">${L.icon('dots', 18)}</button>
    </div>
    ${opts.badge ? `<div style="margin-top: 10px;">${opts.badge}</div>` : ''}
  </div>`;

// ============ UX-09 mobile ============
export const m09 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Обработка нового лида', back: true, sub: 'Черновик v4 · не опубликован', right: 'dots' })}
  ${L.mTabs(["Основное", "Участники", "Шаги", "Решения"], "Шаги")}

  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 10px; background: ${T.subtle}; overflow: hidden;">
    ${step(1, 'Приём входных данных', 'Менеджер продаж', 'Координатор принимает данные лида. Таймаут: 10 мин.')}
    ${step(2, 'Исследовать компанию', 'Аналитик продаж', 'Результат: подтверждённые факты и риски. Таймаут: 30 мин.')}

    <div style="border: 1px dashed ${T.lineStrong}; border-radius: 12px; background: ${T.bg}; padding: 11px;">
      <div style="display: flex; align-items: center; gap: 9px; padding: 0 2px 9px;">
        <span style="flex: 1 1 auto; font-size: 11.5px; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.mut};">Шаг 3 · параллельно · 2 исполнения</span>
        <span style="color: ${T.mut}; display: flex; transform: rotate(90deg);">${L.icon('chevR', 15)}</span>
      </div>
      <div style="display: flex; flex-direction: column; gap: 10px;">
        ${step('3a', 'Подготовить предложение', 'Редактор предложений', 'Результат: документ предложения.')}
        ${step('3b', 'Оценить риски', 'Юридический консультант', 'Вход: условия сделки.', { warn: true, err: 'Не задано описание результата' })}
      </div>
    </div>

    ${step(4, 'Согласовать предложение', '', 'Утвердить · Запросить изменения · Отклонить', { badge: L.statusPill('gate', 'Решение человека', 'sm') })}  </div>

  ${L.mBottom(`
    <div style="display: flex; align-items: center; gap: 8px; font-size: 12px; color: ${T.warn};"><span style="display: flex;">${L.icon("alert", 14)}</span>1 замечание — публикация недоступна</div>
    <div style="display: flex; gap: 8px;">
      <button style="flex: 1 1 0; height: 48px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 15px; font-weight: 500;">Проверить</button>
      <button style="flex: 1 1 0; height: 48px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.field}; color: ${T.faint}; font: inherit; font-size: 15px;">Опубликовать v4</button>
    </div>`)}
`));

// ============ UX-10 mobile ============
const mField = (label, value) => `
  <label style="display: flex; flex-direction: column; gap: 6px;">
    <span style="font-size: 12px; color: ${T.sec};">${label}</span>
    <span style="min-height: 46px; display: flex; align-items: center; padding: 0 13px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; font-size: 14px; color: ${T.ink};">${value}</span>
  </label>`;

export const m10 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Новый запуск', back: true, sub: 'Корпоративные продажи', right: 'none' })}

  <div style="flex: 0 0 auto; display: flex; align-items: center; gap: 8px; padding: 12px 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <span style="display: flex; align-items: center; gap: 7px; font-size: 12.5px; color: ${T.accDark}; font-weight: 600;"><span style="width: 20px; height: 20px; border-radius: 10px; background: ${T.acc}; color: #FFFFFF; font-size: 11px; display: flex; align-items: center; justify-content: center;">1</span>Цель</span>
    <span style="flex: 1 1 auto; height: 2px; background: ${T.line};"></span>
    <span style="display: flex; align-items: center; gap: 7px; font-size: 12.5px; color: ${T.mut};"><span style="width: 20px; height: 20px; border-radius: 10px; border: 1px solid ${T.lineStrong}; font-size: 11px; display: flex; align-items: center; justify-content: center;">2</span>Данные</span>
    <span style="flex: 1 1 auto; height: 2px; background: ${T.line};"></span>
    <span style="display: flex; align-items: center; gap: 7px; font-size: 12.5px; color: ${T.mut};"><span style="width: 20px; height: 20px; border-radius: 10px; border: 1px solid ${T.lineStrong}; font-size: 11px; display: flex; align-items: center; justify-content: center;">3</span>Проверка</span>
  </div>

  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">
    <div>
      <div style="font-size: 12px; color: ${T.sec}; margin-bottom: 7px;">Что запускаем</div>
      <div style="display: flex; padding: 4px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line};">
        <span style="flex: 1 1 0; height: 40px; display: flex; align-items: center; justify-content: center; border-radius: 8px; font-size: 13.5px; color: ${T.sec};">ИИ-сотрудник</span>
        <span style="flex: 1 1 0; height: 40px; display: flex; align-items: center; justify-content: center; border-radius: 8px; background: ${T.bg}; font-size: 13.5px; font-weight: 600; box-shadow: 0 1px 2px rgba(16,22,30,0.08);">Процесс</span>
      </div>
    </div>

    ${mField('Процесс', 'Обработка нового лида · v3')}

    <label style="display: flex; flex-direction: column; gap: 6px;">
      <span style="font-size: 12px; color: ${T.sec};">Задача</span>
      <span style="min-height: 80px; display: block; padding: 12px 13px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; font-size: 14px; line-height: 1.5;">Подготовить коммерческое предложение для компании Север к встрече 27 августа</span>
    </label>

    ${mField('Компания', 'Север')}
    ${L.mCard(`
      <div style="display: flex; align-items: center; gap: 11px;">
        <span style="color: ${T.sec}; display: flex;">${L.icon('file', 19)}</span>
        <span style="flex: 1 1 auto; min-width: 0;">
          <span style="display: block; font-size: 13.5px; font-weight: 500;">brief.pdf</span>
          <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">1,8 МБ</span>
        </span>
        ${L.statusPill('done', 'Проверен', 'sm')}
      </div>`, `border-color: ${T.okLine}; background: ${T.okSoft};`)}  </div>

  ${L.mBottom(`
    <div style="font-size: 12px; color: ${T.mut};">Участники: Аналитик продаж, Редактор предложений, Юридический консультант · 1 решение человека · интеграции не требуются</div>
    ${L.mBtn('Запустить', 'pri')}
    ${L.mBtn('Сохранить черновик', 'sec')}`)}
`));
