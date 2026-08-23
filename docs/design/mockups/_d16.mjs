import * as L from './_lib.mjs';
const T = L.T;

const gateRow = (title, project, run, requester, pill, ago, sel) => `
  <div style="padding: 13px 16px; border-top: 1px solid ${T.row}; ${sel ? `background: ${T.accTint}; box-shadow: inset 2px 0 0 ${T.acc};` : ''}">
    <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 10px;">
      <span style="font-size: 13.5px; font-weight: ${sel ? 600 : 500}; line-height: 1.35;">${title}</span>
      ${pill}
    </div>
    <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 5px; line-height: 1.45;">${project} · ${run}</div>
    <div style="display: flex; align-items: center; gap: 10px; margin-top: 6px; font-size: 11.5px; color: ${T.mut};">
      <span>Запросил: <span style="color: ${T.sec};">${requester}</span></span><span>·</span><span>${ago}</span>
    </div>
  </div>`;

const block = (title, inner) => `
  <div style="border: 1px solid ${T.line}; border-radius: 11px; background: ${T.bg}; padding: 14px 16px;">
    <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">${title}</div>
    <div style="margin-top: 10px;">${inner}</div>
  </div>`;

// ============ UX-16 Решения ============
export const d16 = L.page(L.shellDesktop({
  nav: 'decisions', project: 'Все проекты', navOpts: { decisions: 2 },
  body: `
  ${L.contentHead({
    path: ['Решения'],
    title: 'Решения',
    sub: 'Вопросы, без вашего ответа работа не продолжится',
  })}

  <div style="flex: 1 1 auto; display: flex; gap: 18px; padding: 16px 24px 22px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 0 0 396px; display: flex; flex-direction: column; gap: 12px; min-height: 0;">
      <div style="display: flex; align-items: center; gap: 8px;">
        ${L.chip('Ожидают меня', true)}${L.chip('Все проекты')}${L.chip('Сначала срочные')}
      </div>
      <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
        ${gateRow('Согласовать коммерческое предложение', 'Корпоративные продажи', 'Подготовить предложение для компании Север',
          'Менеджер продаж', L.statusPill('gate', 'Ожидает вас', 'sm'), 'открыт 8 минут назад', true)}
        ${gateRow('Разрешить обновление этапа сделки в CRM', 'Корпоративные продажи', 'Обработка нового лида',
          'Аналитик продаж', L.statusPill('wait', 'Изменение во внешней системе', 'sm'), 'срок: сегодня, 18:00', false)}
        <div style="padding: 13px 16px; border-top: 1px solid ${T.row}; font-size: 11.5px; color: ${T.mut}; line-height: 1.5;">
          Решение принимается один раз: если его уже принял другой участник, вы увидите фактический результат.
        </div>
      </div>
    </div>

    <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; display: flex; flex-direction: column; min-width: 0; overflow: hidden;">
      <div style="padding: 16px 20px; border-bottom: 1px solid ${T.line};">
        <div style="font-size: 11.5px; color: ${T.mut};"><a href="#">Запуск: Подготовить предложение для компании Север</a></div>
        <div style="display: flex; align-items: center; gap: 12px; margin-top: 7px;">
          <h2 style="margin: 0; font-size: 17px; font-weight: 600;">Согласовать коммерческое предложение</h2>
          ${L.statusPill('gate', 'Процесс приостановлен')}
        </div>
        <div style="display: flex; align-items: center; gap: 10px; margin-top: 7px; font-size: 12px; color: ${T.mut};">
          <span>Запросил: <span style="color: ${T.sec};">Менеджер продаж</span></span><span>·</span>
          <span>Срок: <span style="color: ${T.sec};">сегодня, 18:00</span></span><span>·</span>
          <span>Данные версии: <span style="color: ${T.sec};">v3</span></span>
        </div>
      </div>

      <div style="flex: 1 1 auto; padding: 16px 20px; display: flex; flex-direction: column; gap: 14px; min-height: 0;">
        ${block('Что подготовлено', `
          <p style="margin: 0; font-size: 13px; line-height: 1.6; color: ${T.ink2};">Предложение на пилот для 50 сотрудников: состав работ, сроки внедрения и стоимость пилотного периода. Факты о клиенте подтверждены исследованием, оценка рисков от юридического консультанта ещё не получена.</p>`)}

        <div style="display: flex; gap: 14px;">
          <div style="flex: 1 1 0;">${block('Файлы', `
            <div style="display: flex; align-items: center; gap: 11px;">
              <span style="color: ${T.sec}; display: flex;">${L.icon('file', 18)}</span>
              <span style="flex: 1 1 auto;">
                <span style="display: block; font-size: 13px; font-weight: 500;">Коммерческое предложение.pdf</span>
                <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">1,4 МБ · безопасный предпросмотр</span>
              </span>
              ${L.btn('Открыть', 'sec', 28)}
            </div>`)}</div>
          <div style="flex: 1 1 0;">${block('Последствия', `
            <div style="display: flex; flex-direction: column; gap: 7px; font-size: 12.5px; color: ${T.ink2}; line-height: 1.45;">
              <span>Утверждение продолжит Процесс и передаст результат координатору.</span>
              <span>Запрос изменений создаст следующий ход для ИИ-сотрудника.</span>
              <span>Отклонение остановит этот путь Процесса.</span>
            </div>`)}</div>
        </div>

        ${L.textarea('Комментарий', '<span style="color: ' + T.faint + '">Необязательно. Будет сохранён в аудите вместе с решением.</span>', 58)}
      </div>

      <div style="padding: 14px 20px 18px; border-top: 1px solid ${T.line}; display: flex; align-items: center; gap: 10px;">
        <button style="height: 40px; padding: 0 24px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 14px; font-weight: 600;">Утвердить</button>
        ${L.btn('Запросить изменения', 'sec', 40)}
        ${L.btn('Отклонить', 'danger', 40)}
        <div style="flex: 1 1 auto;"></div>
        <span style="font-size: 11.5px; color: ${T.mut};">Решение будет записано на ваше имя</span>
      </div>
    </div>
  </div>`,
}));
