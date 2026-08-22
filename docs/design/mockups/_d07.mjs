import * as L from './_lib.mjs';
const T = L.T;

const tabs = (items, active) => `
  <div style="display: flex; gap: 4px; padding: 0 24px; border-bottom: 1px solid ${T.line}; background: ${T.bg}; flex: 0 0 42px; align-items: stretch;">
    ${items.map((t) => `<span style="display: flex; align-items: center; padding: 0 12px; font-size: 12.5px; ${t === active ? `color: ${T.ink}; font-weight: 600; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${t}</span>`).join('')}
  </div>`;

const sumRow = (k, v) => `<div style="display: flex; gap: 12px; padding: 9px 0; border-top: 1px solid ${T.row};"><span style="flex: 0 0 118px; font-size: 12px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${v}</span></div>`;

// ============ UX-07 ИИ-сотрудник ============
export const d07 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('agents')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'ИИ-сотрудники', 'Аналитик продаж'],
    title: 'Аналитик продаж',
    sub: 'Специалист по исследованию клиентов',
    pill: `${L.statusPill('done', 'Готов')}<span style="display: inline-flex; align-items: center; height: 24px; padding: 0 10px; border-radius: 12px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11.5px; color: ${T.sec};">Опубликованы инструкции v3</span>`,
    actions: `${L.btn('Изменить', 'sec', 36)}<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Запустить</button>${L.iconBtn('dots', 'Другие действия', 36)}`,
  })}
  ${tabs(['Обзор', 'Инструкции', 'Возможности', 'Интеграции', 'Знания', 'Версии', 'Аудит'], 'Инструкции')}

  <div style="flex: 1 1 auto; display: flex; gap: 18px; padding: 18px 24px 22px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 14px; min-width: 0;">
      <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px;">
        <div style="display: flex; align-items: center; gap: 12px;">
          <h2 style="margin: 0; font-size: 15px; font-weight: 600;">Черновик v4</h2>
          <span style="display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 10px; border-radius: 12px; background: ${T.warnTint}; border: 1px solid ${T.warnLine}; color: ${T.warn}; font-size: 11.5px; font-weight: 500;">${L.icon('alert', 12)}Есть неопубликованные изменения</span>
        </div>
        <a href="#" style="font-size: 12.5px;">Посмотреть опубликованную v3</a>
      </div>

      <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; display: flex; flex-direction: column; min-height: 0; overflow: hidden;">
        <div style="display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; border-bottom: 1px solid ${T.hair}; background: ${T.field};">
          <span style="font-size: 11.5px; color: ${T.sec};">Инструкции ИИ-сотрудника</span>
          <span style="font-family: ${T.mono}; font-size: 11px; color: ${T.mut};">черновик · автосохранение</span>
        </div>
        <div style="flex: 1 1 auto; padding: 16px 18px; font-size: 13.5px; line-height: 1.75; color: ${T.ink};">
          Изучи предоставленные сведения о клиенте. Отделяй подтверждённые факты от предположений. Подготовь краткое резюме, потребности, риски и вопросы для уточнения. Не отправляй данные во внешние системы без явного разрешения.
        </div>
        <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; border-top: 1px solid ${T.hair};">
          <span style="font-size: 11.5px; color: ${T.mut};">Опубликованная версия неизменяема. Публикация создаст v4 и оставит v3 в истории.</span>
          <span style="display: flex; gap: 8px;">
            ${L.btn('Отменить черновик', 'ghost', 34)}
            ${L.btn('Проверить инструкции', 'sec', 34)}
            <button style="height: 34px; padding: 0 16px; border-radius: 8px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 12.5px; font-weight: 600;">Опубликовать v4</button>
          </span>
        </div>
      </div>

      <div style="border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 14px 16px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Проверка инструкций</div>
        <div style="display: flex; gap: 26px; margin-top: 11px;">
          <span style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 15)}</span>Структура корректна</span>
          <span style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 15)}</span>2 возможности используются</span>
          <span style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 15)}</span>Секретные значения не обнаружены</span>
        </div>
      </div>
    </div>

    <aside style="flex: 0 0 340px; display: flex; flex-direction: column; gap: 14px;">
      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
        <div style="display: flex; align-items: center; gap: 11px;">
          ${L.avatar('АП', 36)}
          <span style="display: flex; flex-direction: column;">
            <span style="font-size: 14px; font-weight: 600;">Аналитик продаж</span>
            <span style="font-size: 11.5px; color: ${T.mut};">Специалист по исследованию клиентов</span>
          </span>
        </div>
        <div style="margin-top: 14px;">
          ${sumRow('Назначение', 'Исследование клиента и подготовка фактов')}
          ${sumRow('Runtime', 'Стандартный рабочий runtime')}
          ${sumRow('Возможности', 'Работа с файлами · Анализ данных')}
          ${sumRow('Интеграции', 'Не подключены')}
          ${sumRow('Знания', 'Описание продукта · v2')}
          ${sumRow('Изменил', 'Анна Волкова · сегодня, 10:24')}
        </div>
      </div>

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Версии</div>
        <div style="display: flex; flex-direction: column; margin-top: 10px;">
          <div style="display: flex; align-items: center; gap: 10px; padding: 9px 0; border-top: 1px solid ${T.row};">
            <span style="font-family: ${T.mono}; font-size: 12px; color: ${T.warn}; font-weight: 600;">v4</span>
            <span style="flex: 1 1 auto; font-size: 12.5px;">Черновик</span>
            <span style="font-size: 11.5px; color: ${T.mut};">сегодня</span>
          </div>
          <div style="display: flex; align-items: center; gap: 10px; padding: 9px 0; border-top: 1px solid ${T.row};">
            <span style="font-family: ${T.mono}; font-size: 12px; color: ${T.ok}; font-weight: 600;">v3</span>
            <span style="flex: 1 1 auto; font-size: 12.5px;">Опубликована</span>
            <a href="#" style="font-size: 11.5px;">Сравнить</a>
          </div>
          <div style="display: flex; align-items: center; gap: 10px; padding: 9px 0; border-top: 1px solid ${T.row};">
            <span style="font-family: ${T.mono}; font-size: 12px; color: ${T.mut}; font-weight: 600;">v2</span>
            <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.sec};">Архивная</span>
            <a href="#" style="font-size: 11.5px;">Вернуть</a>
          </div>
        </div>
        <p style="margin: 12px 0 0; font-size: 11.5px; color: ${T.mut}; line-height: 1.5;">Возврат создаёт новый черновик от выбранной версии и не перезаписывает историю.</p>
      </div>
    </aside>
  </div>`,
}));
