import * as L from './_lib.mjs';
const T = L.T;

const swatch = (hex, name, role) => `
  <div style="display: flex; align-items: center; gap: 9px;">
    <span style="width: 26px; height: 26px; flex: 0 0 26px; border-radius: 7px; background: ${hex}; border: 1px solid rgba(16,22,30,0.12);"></span>
    <span style="min-width: 0;">
      <span style="display: block; font-size: 11.5px; font-weight: 500;">${name}</span>
      <span style="display: block; font-family: ${T.mono}; font-size: 10px; color: ${T.mut};">${hex} · ${role}</span>
    </span>
  </div>`;

const idxRow = (id, name, scope) => `
  <div style="display: flex; align-items: baseline; gap: 9px; padding: 4px 0;">
    <span style="flex: 0 0 34px; font-family: ${T.mono}; font-size: 11px; color: ${T.mut};">${id}</span>
    <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink};">${name}</span>
    <span style="flex: 0 0 auto; font-size: 10.5px; color: ${T.mut};">${scope}</span>
  </div>`;

export const cover = (screens) => L.page(L.frameDesktop(`
  <div style="width: 100%; display: flex; flex-direction: column; background: ${T.subtle};">

    <div style="flex: 0 0 auto; padding: 34px 40px 26px; background: ${T.bg}; border-bottom: 1px solid ${T.line};">
      <div style="display: flex; align-items: center; gap: 12px;">
        <span style="width: 32px; height: 32px; border-radius: 9px; background: ${T.acc}; display: flex; align-items: center; justify-content: center;">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#FFFFFF" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M4 18V7l8 5 8-5v11"/></svg>
        </span>
        <span style="font-size: 15px; font-weight: 600; letter-spacing: -0.01em;">MatterCodex</span>
        <span style="display: inline-flex; align-items: center; height: 24px; padding: 0 11px; border-radius: 12px; background: ${T.accTint}; border: 1px solid ${T.accLine}; color: ${T.accDark}; font-size: 11.5px;">web-first reset · UX-MC-002</span>
      </div>
      <h1 style="margin: 18px 0 0; font-size: 34px; font-weight: 600; letter-spacing: -0.03em;">Макеты интерфейса · 19 экранов</h1>
      <p style="margin: 9px 0 0; font-size: 15px; color: ${T.sec}; max-width: 760px; line-height: 1.55;">Каждый экран собран в двух размерах: desktop 1440×1024 и mobile 390×844. Один экран — один HTML-файл. Дизайн-система: вариант А, светлая тема.</p>
    </div>

    <div style="flex: 1 1 auto; display: flex; gap: 22px; padding: 24px 40px 30px; min-height: 0;">

      <div style="flex: 0 0 300px; display: flex; flex-direction: column; gap: 16px;">
        <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Как устроен холст</div>
          <div style="display: flex; align-items: center; gap: 12px; margin-top: 14px;">
            <span style="width: 96px; height: 68px; border: 1px solid ${T.lineStrong}; border-radius: 5px; background: ${T.canvas};"></span>
            <span style="width: 26px; height: 56px; border: 1px solid ${T.lineStrong}; border-radius: 5px; background: ${T.canvas};"></span>
            <span style="flex: 1 1 auto; font-size: 11.5px; color: ${T.sec}; line-height: 1.5;">Слева desktop, справа mobile. Один ряд — один экран, ряды идут в порядке UX-01…UX-19.</span>
          </div>
          <div style="margin-top: 14px; padding-top: 14px; border-top: 1px solid ${T.row}; font-size: 11.5px; color: ${T.sec}; line-height: 1.6;">
            Имя файла: <span style="font-family: ${T.mono}; color: ${T.ink};">NN_slug_desktop</span> и <span style="font-family: ${T.mono}; color: ${T.ink};">NN_slug_mobile</span>. Полный перечень с маршрутами — в <span style="font-family: ${T.mono}; color: ${T.ink};">index.md</span>.
          </div>
        </div>

        <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Палитра</div>
          <div style="display: flex; flex-direction: column; gap: 11px; margin-top: 13px;">
            ${swatch(T.acc, 'Акцент', 'действие, активное состояние')}
            ${swatch(T.ok, 'Успех', 'готов, завершён, в сети')}
            ${swatch(T.warn, 'Внимание', 'решение человека, ожидание')}
            ${swatch(T.err, 'Ошибка', 'сбой, изоляция, отмена')}
            ${swatch(T.ink, 'Текст', 'основной')}
            ${swatch(T.line, 'Границы', 'разделители и рамки')}
          </div>
        </div>
      </div>

      <div style="flex: 0 0 300px; display: flex; flex-direction: column; gap: 16px;">
        <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Типографика</div>
          <div style="margin-top: 13px; display: flex; flex-direction: column; gap: 10px;">
            <span style="font-size: 19px; font-weight: 600; letter-spacing: -0.015em;">Заголовок экрана · 19/600</span>
            <span style="font-size: 13px;">Основной текст интерфейса · 13/400</span>
            <span style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Заголовок секции · 12,5/600</span>
            <span style="font-family: ${T.mono}; font-size: 12px; color: ${T.sec};">Числа и время · IBM Plex Mono</span>
          </div>
          <div style="margin-top: 13px; padding-top: 13px; border-top: 1px solid ${T.row}; font-size: 11.5px; color: ${T.mut}; line-height: 1.5;">
            IBM Plex Sans 400/500/600 + IBM Plex Mono. Табличные цифры включены везде.
          </div>
        </div>

        <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Состояния</div>
          <div style="display: flex; flex-wrap: wrap; gap: 8px; margin-top: 13px;">
            ${L.statusPill('run', 'Выполняется')}
            ${L.statusPill('done', 'Готов')}
            ${L.statusPill('wait', 'Ожидает')}
            ${L.statusPill('gate', 'Решение человека')}
            ${L.statusPill('err', 'Ошибка')}
            ${L.statusPill('off', 'Отключён')}
          </div>
          <div style="margin-top: 14px; padding-top: 14px; border-top: 1px solid ${T.row}; font-size: 11.5px; color: ${T.sec}; line-height: 1.6;">
            Статус всегда передаётся текстом и иконкой, а не только цветом. Кнопки «Обновить» нет ни на одном экране: данные приходят сами.
          </div>
        </div>
      </div>

      <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px 18px; display: flex; flex-direction: column; min-width: 0;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Перечень экранов</div>
        <div style="flex: 1 1 auto; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 26px; margin-top: 10px; align-content: start;">
          ${screens.map(([id, , name, , scope]) => idxRow(`UX-${id}`, name, scope)).join('')}
        </div>
        <div style="padding-top: 12px; border-top: 1px solid ${T.row}; font-size: 11.5px; color: ${T.mut}; line-height: 1.6;">
          Показано основное ready-состояние каждого экрана. Отдельные состояния (loading, empty, error, forbidden, offline, conflict) описаны в исходном пакете промптов и в макеты пока не вынесены.
        </div>
      </div>
    </div>
  </div>
`));
