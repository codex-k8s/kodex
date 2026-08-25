import * as L from './_lib.mjs';
const T = L.T;

const step = (n, label, active) => `
  <span style="display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: ${active ? T.accDark : T.mut};">
    <span style="width: 20px; height: 20px; border-radius: 10px; ${active ? `background: ${T.acc}; color: #FFFFFF;` : `border: 1px solid ${T.lineStrong};`} font-size: 11px; font-weight: 600; display: flex; align-items: center; justify-content: center;">${n}</span>${label}
  </span>`;

const guarantee = (t) => `<span style="display: flex; align-items: flex-start; gap: 9px; font-size: 13px; color: ${T.ink2}; line-height: 1.45;"><span style="color: ${T.ok}; flex: 0 0 auto; display: flex; padding-top: 1px;">${L.icon('check', 15)}</span>${t}</span>`;

// ============ UX-01 mobile ============
export const m01 = L.page(L.frameMobile(`
  <header style="flex: 0 0 56px; display: flex; align-items: center; justify-content: space-between; padding: 0 16px; border-bottom: 1px solid ${T.line};">
    <span style="display: flex; align-items: center; gap: 9px;">
      <span style="width: 24px; height: 24px; border-radius: 6px; background: ${T.acc}; display: flex; align-items: center; justify-content: center;">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#FFFFFF" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M4 18V7l8 5 8-5v11"/></svg>
      </span>
      <span style="font-size: 15px; font-weight: 600;">Kodex</span>
    </span>
    <button aria-label="Меню пользователя: Анна Волкова, Владелец" style="height: 44px; display: flex; align-items: center; gap: 7px; border: 0; background: none; margin-right: -8px; padding: 0 8px;">
      ${L.avatar('АВ', 30)}<span style="color: ${T.mut}; display: flex;">${L.icon('chev', 15)}</span>
    </button>
  </header>

  <div style="flex: 1 1 auto; padding: 16px; display: flex; flex-direction: column; gap: 16px; background: ${T.subtle}; overflow: hidden;">
    <div>
      <div style="font-size: 12px; color: ${T.sec};">Шаг 1 из 3 · Начало работы</div>
      <div style="display: flex; align-items: center; gap: 10px; margin-top: 10px;">
        ${step(1, 'Цель', true)}
        <span style="flex: 1 1 auto; height: 2px; background: ${T.line};"></span>
        ${step(2, '', false)}
        <span style="flex: 1 1 auto; height: 2px; background: ${T.line};"></span>
        ${step(3, '', false)}
      </div>
    </div>

    <div>
      <h1 style="margin: 0; font-size: 24px; font-weight: 600; letter-spacing: -0.02em; line-height: 1.2;">Добро пожаловать в Kodex</h1>
      <p style="margin: 8px 0 0; font-size: 14px; color: ${T.sec}; line-height: 1.5;">Создайте первый Проект и ИИ-сотрудника. Интеграции можно подключить позже.</p>
    </div>

    ${L.mCard(`
      <div style="display: flex; align-items: center; gap: 11px;">
        <span style="width: 34px; height: 34px; flex: 0 0 34px; border-radius: 10px; background: ${T.accTint}; border: 1px solid ${T.accSoftLine}; color: ${T.acc}; display: flex; align-items: center; justify-content: center;">${L.icon('bot', 19)}</span>
        <span style="flex: 1 1 auto; min-width: 0;">
          <span style="display: block; font-size: 14.5px; font-weight: 600;">Помощник Kodex</span>
          <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">Системный · неудаляемый</span>
        </span>
      </div>
      <div style="margin-top: 12px;">${L.statusPill('done', 'Готов к работе')}</div>
      <p style="margin: 10px 0 0; font-size: 12.5px; color: ${T.sec}; line-height: 1.5;">Действует от вашего имени и только в пределах ваших прав.</p>`)}

    ${L.mCard(`
      <div style="font-size: 16px; font-weight: 600;">Начать с помощником</div>
      <p style="margin: 8px 0 14px; font-size: 13px; color: ${T.sec}; line-height: 1.5;">Расскажите, какую работу хотите организовать. Изменения применятся только после вашего подтверждения.</p>
      ${L.mBtn('Начать с помощником', 'pri')}`, `border: 1.5px solid ${T.acc};`)}

    ${L.mCard(`
      <div style="font-size: 15px; font-weight: 600; margin-bottom: 12px;">Настроить вручную · пошаговые формы</div>
      ${L.mBtn('Настроить вручную', 'sec')}`)}

    <div style="display: flex; flex-direction: column; gap: 8px; padding: 0 2px;">
      ${guarantee('Можно работать без интеграций')}
      ${guarantee('Все изменения показываются до применения')}
      ${guarantee('Действия сохраняются в аудите')}
    </div>
  </div>
`));

// ============ UX-02 mobile ============
export const m02 = L.page(L.frameMobile(`
  <header style="flex: 0 0 56px; display: flex; align-items: center; gap: 6px; padding: 0 16px; border-bottom: 1px solid ${T.line};">
    <button aria-label="Меню" style="width: 44px; height: 44px; margin-left: -10px; border: 0; background: none; color: ${T.ink}; display: flex; align-items: center; justify-content: center;">${L.icon('menu', 22)}</button>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 15px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">Помощник Kodex</span>
      <span style="display: flex; align-items: center; gap: 5px; font-size: 11.5px; color: ${T.ok};"><span style="width: 6px; height: 6px; border-radius: 3px; background: ${T.ok};"></span>Готов</span>
    </span>
    <button aria-label="История диалогов" style="width: 44px; height: 44px; margin-right: -10px; border: 0; background: none; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('list', 20)}</button>
  </header>

  <div style="flex: 0 0 auto; padding: 10px 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <button style="width: 100%; height: 40px; display: flex; align-items: center; gap: 9px; padding: 0 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px;">
      <span style="width: 6px; height: 6px; border-radius: 3px; background: ${T.acc};"></span>Корпоративные продажи
      <span style="margin-left: auto; color: ${T.mut}; display: flex;">${L.icon('chev', 16)}</span>
    </button>
  </div>

  <div style="flex: 1 1 auto; padding: 13px 14px; display: flex; flex-direction: column; gap: 12px; background: ${T.subtle}; overflow: hidden;">
    <div style="align-self: flex-end; max-width: 300px;">
      <div style="font-size: 11px; color: ${T.mut}; text-align: right; margin-bottom: 4px;">Вы · 10:08</div>
      <div style="padding: 11px 13px; border-radius: 12px 12px 4px 12px; background: ${T.bg}; border: 1px solid ${T.line}; font-size: 13.5px; line-height: 1.5;">Создай проект для отдела продаж и агента, который будет анализировать материалы клиентов</div>
    </div>
    <div style="max-width: 320px;">
      <div style="font-size: 11px; color: ${T.mut}; margin-bottom: 4px;">Помощник · 10:08</div>
      <div style="padding: 11px 13px; border-radius: 12px 12px 12px 4px; background: ${T.accSoft}; border: 1px solid ${T.accSoftLine}; font-size: 13.5px; line-height: 1.5;">Я подготовил безопасный план. Внешние интеграции не требуются.</div>
    </div>

    ${L.mCard(`
      <div style="display: flex; align-items: center; gap: 9px;">
        <span style="color: ${T.mut}; display: flex; transform: rotate(90deg);">${L.icon('chevR', 15)}</span>
        <span style="flex: 1 1 auto; font-size: 14px; font-weight: 600;">План изменений · 2 действия</span>
        <span style="font-size: 11.5px; color: ${T.warn};">не применён</span>
      </div>
      <div style="margin-top: 12px; display: flex; flex-direction: column; gap: 9px;">
        <span style="display: flex; align-items: flex-start; gap: 9px; font-size: 13px;"><span style="width: 18px; height: 18px; flex: 0 0 18px; border-radius: 9px; background: ${T.acc}; color: #FFFFFF; font-size: 10.5px; font-weight: 700; display: flex; align-items: center; justify-content: center;">1</span>Создать Проект «Корпоративные продажи»</span>
        <span style="display: flex; align-items: flex-start; gap: 9px; font-size: 13px;"><span style="width: 18px; height: 18px; flex: 0 0 18px; border-radius: 9px; background: ${T.acc}; color: #FFFFFF; font-size: 10.5px; font-weight: 700; display: flex; align-items: center; justify-content: center;">2</span>Создать ИИ-сотрудника «Аналитик продаж»</span>
      </div>
      <div style="margin-top: 12px; padding: 10px 11px; border-radius: 9px; background: ${T.subtle}; border: 1px solid ${T.line}; font-size: 12px; color: ${T.sec}; line-height: 1.5;">
        Работа с файлами · runtime «Стандартный» · интеграции: нет<br>В аудит: Анна Волкова через Помощника Kodex
      </div>
      <div style="margin-top: 12px; display: flex; flex-direction: column; gap: 8px;">
        ${L.mBtn('Применить изменения', 'pri')}
        <div style="display: flex; gap: 8px;">
          <button style="flex: 1 1 0; height: 44px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px;">Изменить план</button>
          <button style="flex: 1 1 0; height: 44px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 13.5px;">Отменить</button>
        </div>
      </div>`)}
  </div>

  <div style="flex: 0 0 auto; padding: 10px 16px 14px; border-top: 1px solid ${T.line}; background: ${T.bg}; display: flex; align-items: center; gap: 9px;">
    <button aria-label="Прикрепить файл" style="width: 44px; height: 44px; flex: 0 0 44px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('upload', 18)}</button>
    <span style="flex: 1 1 auto; height: 44px; display: flex; align-items: center; padding: 0 12px; border-radius: 10px; border: 1px solid ${T.line}; color: ${T.faint}; font-size: 13.5px;">Опишите задачу или настройку…</span>
    <button aria-label="Отправить" style="width: 44px; height: 44px; flex: 0 0 44px; border-radius: 10px; border: 0; background: ${T.acc}; color: #FFFFFF; display: flex; align-items: center; justify-content: center;">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h13M12 5l7 7-7 7"/></svg>
    </button>
  </div>
`));
