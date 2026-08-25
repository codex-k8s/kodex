import * as L from './_lib.mjs';
const T = L.T;

// ============ UX-02 Помощник Kodex ============
const histItem = (t, sub, active) => `
  <a href="#" style="display: flex; flex-direction: column; gap: 3px; padding: 10px 12px; border-radius: 8px; ${active ? `background: ${T.accTint}; border: 1px solid ${T.accLine};` : `border: 1px solid transparent;`} color: ${T.ink};">
    <span style="font-size: 12.5px; font-weight: ${active ? 600 : 500}; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${t}</span>
    <span style="font-size: 11px; color: ${T.mut};">${sub}</span>
  </a>`;

const opRow = (n, title, body) => `
  <div style="border: 1px solid ${T.line}; border-radius: 10px; background: ${T.bg}; overflow: hidden;">
    <div style="display: flex; align-items: center; gap: 9px; padding: 9px 12px; background: ${T.field}; border-bottom: 1px solid ${T.line};">
      <span style="width: 18px; height: 18px; border-radius: 9px; background: ${T.acc}; color: #FFFFFF; font-size: 10.5px; font-weight: 700; display: flex; align-items: center; justify-content: center;">${n}</span>
      <span style="font-size: 12.5px; font-weight: 600;">${title}</span>
    </div>
    ${body ? `<div style="padding: 10px 12px; display: flex; flex-direction: column; gap: 7px;">${body}</div>` : ''}
  </div>`;

const kv = (k, v) => `<div style="display: flex; gap: 10px; font-size: 12px;"><span style="flex: 0 0 104px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; color: ${T.ink2};">${v}</span></div>`;

export const d02 = L.page(L.shellDesktop({
  nav: 'assistant', project: 'Корпоративные продажи',
  body: `
  <div style="flex: 1 1 auto; display: flex; min-height: 0;">

    <section style="width: 260px; flex: 0 0 260px; display: flex; flex-direction: column; border-right: 1px solid ${T.line}; background: ${T.subtle};">
      <div style="padding: 14px 12px 10px;">
        <button style="width: 100%; height: 34px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px; font-weight: 500; display: flex; align-items: center; justify-content: center; gap: 7px;">${L.icon('plus', 15)}Новый диалог</button>
      </div>
      <div style="padding: 0 12px 8px;">
        <div style="font-size: 11px; letter-spacing: 0.04em; text-transform: uppercase; color: ${T.mut}; padding: 6px 12px;">История диалогов</div>
        <div style="display: flex; flex-direction: column; gap: 2px;">
          ${histItem('Настройка отдела продаж', 'Сегодня, 10:08', true)}
          ${histItem('Почему запуск остановился?', 'Вчера, 17:40', false)}
          ${histItem('Подключение CRM', '20 августа', false)}
        </div>
      </div>
    </section>

    <section style="flex: 1 1 auto; display: flex; flex-direction: column; min-width: 0;">
      <div style="flex: 0 0 66px; display: flex; align-items: center; gap: 12px; padding: 0 24px; border-bottom: 1px solid ${T.line};">
        <span style="width: 34px; height: 34px; border-radius: 10px; background: ${T.accTint}; border: 1px solid ${T.accSoftLine}; color: ${T.acc}; display: flex; align-items: center; justify-content: center;">${L.icon('bot', 19)}</span>
        <span style="display: flex; flex-direction: column;">
          <span style="font-size: 15px; font-weight: 600;">Помощник Kodex</span>
          <span style="font-size: 11px; color: ${T.mut};">Системный · неудаляемый</span>
        </span>
        <span style="margin-left: 12px;">${L.statusPill('done', 'Готов')}</span>
      </div>

      <div style="flex: 1 1 auto; padding: 22px 24px; display: flex; flex-direction: column; gap: 18px; min-height: 0; background: ${T.subtle};">
        <div style="align-self: flex-end; max-width: 560px;">
          <div style="font-size: 11px; color: ${T.mut}; text-align: right; margin-bottom: 5px;">Анна Волкова · 10:08</div>
          <div style="padding: 12px 14px; border-radius: 12px 12px 4px 12px; background: ${T.bg}; border: 1px solid ${T.line}; font-size: 13.5px; line-height: 1.55;">Создай проект для отдела продаж и агента, который будет анализировать материалы клиентов и готовить краткие рекомендации</div>
        </div>
        <div style="align-self: flex-start; max-width: 620px;">
          <div style="font-size: 11px; color: ${T.mut}; margin-bottom: 5px;">Помощник Kodex · 10:08</div>
          <div style="padding: 12px 14px; border-radius: 12px 12px 12px 4px; background: ${T.accSoft}; border: 1px solid ${T.accSoftLine}; font-size: 13.5px; line-height: 1.55; color: ${T.ink};">Я подготовил безопасный план. Внешние интеграции не требуются; их можно добавить позже.</div>
          <div style="display: flex; align-items: center; gap: 8px; margin-top: 10px; font-size: 12px; color: ${T.sec};">
            <span style="color: ${T.acc}; display: flex;">${L.icon('chevR', 14)}</span>План изменений открыт в панели справа — ничего не применено
          </div>
        </div>
      </div>

      <div style="flex: 0 0 auto; padding: 14px 24px 18px; border-top: 1px solid ${T.line}; background: ${T.bg};">
        <div style="border: 1px solid ${T.line}; border-radius: 10px; background: ${T.bg}; overflow: hidden;">
          <div style="min-height: 62px; padding: 11px 13px; font-size: 13px; color: ${T.faint};">Опишите задачу или настройку…</div>
          <div style="display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-top: 1px solid ${T.hair}; background: ${T.subtle};">
            <button aria-label="Прикрепить файл" style="width: 30px; height: 30px; border-radius: 7px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('upload', 15)}</button>
            <span style="display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px; border-radius: 13px; background: ${T.accTint}; border: 1px solid ${T.accLine}; color: ${T.accDark}; font-size: 11.5px;"><span style="width: 6px; height: 6px; border-radius: 3px; background: ${T.acc};"></span>Корпоративные продажи</span>
            <span style="flex: 1 1 auto;"></span>
            <span style="font-size: 11px; color: ${T.mut};">Enter — отправить, Shift+Enter — новая строка</span>
            <button style="height: 30px; padding: 0 16px; border-radius: 8px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 12.5px; font-weight: 500;">Отправить</button>
          </div>
        </div>
      </div>
    </section>

    <aside style="width: 380px; flex: 0 0 380px; display: flex; flex-direction: column; border-left: 1px solid ${T.line}; background: ${T.subtle}; min-height: 0;">
      <div style="padding: 16px 18px 12px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
        <h2 style="margin: 0; font-size: 14.5px; font-weight: 600;">План изменений</h2>
        <div style="display: flex; align-items: center; gap: 8px; margin-top: 8px;">
          <span style="display: inline-flex; align-items: center; gap: 6px; height: 22px; padding: 0 9px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11.5px; color: ${T.sec};">Проект: новый</span>
          <span style="font-size: 11.5px; color: ${T.mut};">2 действия · не применены</span>
        </div>
      </div>

      <div style="flex: 1 1 auto; padding: 14px 18px; display: flex; flex-direction: column; gap: 12px; min-height: 0;">
        ${opRow(1, 'Создать Проект «Корпоративные продажи»', '')}
        ${opRow(2, 'Создать ИИ-сотрудника «Аналитик продаж»', [
          kv('Назначение', 'Анализ материалов клиентов'),
          kv('Инструкции', 'Собрать факты, отделить предположения, подготовить рекомендации'),
          kv('Возможности', 'Работа с загруженными файлами'),
          kv('Runtime', 'Стандартный'),
          kv('Интеграции', 'Нет'),
        ].join(''))}

        <div style="padding: 11px 12px; border-radius: 10px; background: ${T.bg}; border: 1px solid ${T.line}; font-size: 12px; color: ${T.sec}; line-height: 1.5;">
          Будет записано в аудит: <span style="color: ${T.ink};">Анна Волкова через Помощника Kodex</span>
        </div>
      </div>

      <div style="flex: 0 0 auto; padding: 14px 18px 18px; border-top: 1px solid ${T.line}; background: ${T.bg}; display: flex; flex-direction: column; gap: 8px;">
        <button style="width: 100%; height: 38px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Применить изменения</button>
        <div style="display: flex; gap: 8px;">
          <button style="flex: 1 1 0; height: 34px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Изменить план</button>
          <button style="flex: 1 1 0; height: 34px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 12.5px;">Отменить</button>
        </div>
      </div>
    </aside>
  </div>`,
}));
