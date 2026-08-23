import * as L from './_lib.mjs';
const T = L.T;

// ============ UX-01 Первичная настройка ============
export const d01 = L.page(L.frameDesktop(`
  <div style="width: 100%; display: flex; flex-direction: column;">
    <header style="height: 56px; flex: 0 0 56px; display: flex; align-items: center; justify-content: space-between; padding: 0 24px; border-bottom: 1px solid ${T.line};">
      <span style="display: flex; align-items: center; gap: 10px;">
        <span style="width: 24px; height: 24px; border-radius: 6px; background: ${T.acc}; display: flex; align-items: center; justify-content: center;">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#FFFFFF" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M4 18V7l8 5 8-5v11"/></svg>
        </span>
        <span style="font-size: 14px; font-weight: 600; letter-spacing: -0.01em;">MatterCodex</span>
      </span>
      <span style="display: flex; align-items: center; gap: 8px;">
        ${L.avatar('АВ', 28)}
        <span style="display: flex; flex-direction: column;">
          <span style="font-size: 12px;">Анна Волкова</span>
          <span style="font-size: 10.5px; color: ${T.mut};">Владелец</span>
        </span>
        <span style="color: ${T.mut}; display: flex;">${L.icon('chev', 14)}</span>
      </span>
    </header>

    <div style="flex: 1 1 auto; display: flex; justify-content: center; padding: 40px 24px 0; background: ${T.subtle};">
      <div style="width: 1000px; display: flex; flex-direction: column;">

        <div style="display: flex; align-items: center; gap: 14px;">
          <span style="font-size: 12px; color: ${T.sec};">Шаг 1 из 3 · Начало работы</span>
          <span style="flex: 1 1 auto; display: flex; align-items: center; gap: 8px;">
            <span style="display: flex; align-items: center; gap: 7px; font-size: 12px; color: ${T.accDark}; font-weight: 600;"><span style="width: 20px; height: 20px; border-radius: 10px; background: ${T.acc}; color: #FFFFFF; font-size: 11px; display: flex; align-items: center; justify-content: center;">1</span>Цель</span>
            <span style="flex: 1 1 auto; height: 2px; background: ${T.line};"></span>
            <span style="display: flex; align-items: center; gap: 7px; font-size: 12px; color: ${T.mut};"><span style="width: 20px; height: 20px; border-radius: 10px; border: 1px solid ${T.lineStrong}; font-size: 11px; display: flex; align-items: center; justify-content: center;">2</span>Первый ИИ-сотрудник</span>
            <span style="flex: 1 1 auto; height: 2px; background: ${T.line};"></span>
            <span style="display: flex; align-items: center; gap: 7px; font-size: 12px; color: ${T.mut};"><span style="width: 20px; height: 20px; border-radius: 10px; border: 1px solid ${T.lineStrong}; font-size: 11px; display: flex; align-items: center; justify-content: center;">3</span>Первый запуск</span>
          </span>
        </div>

        <h1 style="margin: 26px 0 0; font-size: 30px; font-weight: 600; letter-spacing: -0.025em;">Добро пожаловать в MatterCodex</h1>
        <p style="margin: 8px 0 0; font-size: 15px; color: ${T.sec}; max-width: 640px;">Создайте первый Проект и ИИ-сотрудника. Интеграции можно подключить позже.</p>

        <div style="display: flex; gap: 20px; margin-top: 28px; align-items: stretch;">
          <div style="flex: 1 1 0; display: flex; flex-direction: column; gap: 16px;">
            <div style="border: 1.5px solid ${T.acc}; border-radius: 14px; background: ${T.bg}; padding: 22px; box-shadow: 0 2px 8px rgba(16,22,30,0.06);">
              <div style="display: flex; align-items: center; gap: 12px;">
                <span style="width: 40px; height: 40px; border-radius: 12px; background: ${T.accTint}; color: ${T.acc}; display: flex; align-items: center; justify-content: center;">${L.icon('bot', 22)}</span>
                <span style="font-size: 18px; font-weight: 600;">Начать с Помощником MatterCodex</span>
                <span style="margin-left: auto; font-size: 11px; color: ${T.accDark}; background: ${T.accTint}; border: 1px solid ${T.accLine}; border-radius: 11px; padding: 3px 10px;">Рекомендуем</span>
              </div>
              <p style="margin: 12px 0 0; font-size: 13.5px; color: ${T.sec}; line-height: 1.55;">Расскажите, какую работу хотите организовать. Помощник предложит Проект и ИИ-сотрудника, а изменения будут применены только после вашего подтверждения.</p>
              <button style="margin-top: 18px; height: 42px; padding: 0 22px; border-radius: 10px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 14.5px; font-weight: 600;">Начать с помощником</button>
            </div>

            <div style="border: 1px solid ${T.line}; border-radius: 14px; background: ${T.bg}; padding: 22px;">
              <div style="display: flex; align-items: center; gap: 12px;">
                <span style="width: 40px; height: 40px; border-radius: 12px; background: ${T.field}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('gear', 22)}</span>
                <span style="font-size: 18px; font-weight: 600;">Настроить вручную</span>
              </div>
              <p style="margin: 12px 0 0; font-size: 13.5px; color: ${T.sec}; line-height: 1.55;">Создать Проект и ИИ-сотрудника через пошаговые формы.</p>
              <button style="margin-top: 18px; height: 42px; padding: 0 22px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 14.5px; font-weight: 500;">Настроить вручную</button>
            </div>
          </div>

          <div style="flex: 0 0 340px; display: flex; flex-direction: column; gap: 16px;">
            <div style="border: 1px solid ${T.line}; border-radius: 14px; background: ${T.bg}; padding: 20px;">
              <div style="display: flex; align-items: center; gap: 11px;">
                <span style="width: 34px; height: 34px; border-radius: 10px; background: ${T.accTint}; border: 1px solid ${T.accSoftLine}; color: ${T.acc}; display: flex; align-items: center; justify-content: center;">${L.icon('bot', 19)}</span>
                <span style="display: flex; flex-direction: column;">
                  <span style="font-size: 14px; font-weight: 600;">Помощник MatterCodex</span>
                  <span style="font-size: 11px; color: ${T.mut};">Системный · неудаляемый</span>
                </span>
              </div>
              <div style="margin-top: 14px;">${L.statusPill('done', 'Готов к работе')}</div>
              <p style="margin: 12px 0 0; font-size: 12.5px; color: ${T.sec}; line-height: 1.5;">Действует от вашего имени и только в пределах ваших прав.</p>
            </div>

            <div style="border: 1px solid ${T.line}; border-radius: 14px; background: ${T.bg}; padding: 20px;">
              <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Что гарантировано</div>
              <div style="display: flex; flex-direction: column; gap: 12px; margin-top: 14px;">
                <span style="display: flex; align-items: flex-start; gap: 10px; font-size: 13px; color: ${T.ink2}; line-height: 1.45;"><span style="color: ${T.ok}; flex: 0 0 auto; display: flex; padding-top: 1px;">${L.icon('check', 15)}</span>Можно работать без интеграций</span>
                <span style="display: flex; align-items: flex-start; gap: 10px; font-size: 13px; color: ${T.ink2}; line-height: 1.45;"><span style="color: ${T.ok}; flex: 0 0 auto; display: flex; padding-top: 1px;">${L.icon('check', 15)}</span>Все изменения показываются до применения</span>
                <span style="display: flex; align-items: flex-start; gap: 10px; font-size: 13px; color: ${T.ink2}; line-height: 1.45;"><span style="color: ${T.ok}; flex: 0 0 auto; display: flex; padding-top: 1px;">${L.icon('check', 15)}</span>Действия сохраняются в аудите</span>
              </div>
            </div>

            <p style="margin: 0; font-size: 12px; color: ${T.mut}; line-height: 1.5;">Подключения к внешним системам выполняются позже, в разделе «Интеграции».</p>
          </div>
        </div>
      </div>
    </div>
  </div>
`));
