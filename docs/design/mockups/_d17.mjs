import * as L from './_lib.mjs';
const T = L.T;

const th = (t, w) => `<span style="flex: ${w}; font-size: 11px; letter-spacing: 0.04em; text-transform: uppercase; color: ${T.mut};">${t}</span>`;
const row = (init, name, platform, projectRole, perms, pill, changed, protectedNote) => `
  <div style="display: flex; align-items: center; gap: 14px; padding: 13px 16px; border-top: 1px solid ${T.row};">
    <span style="flex: 1 1 0; min-width: 0; display: flex; align-items: center; gap: 11px;">
      ${L.avatar(init, 30)}
      <span style="min-width: 0;">
        <span style="display: block; font-size: 13.5px; font-weight: 500;">${name}</span>
        ${protectedNote ? `<span style="display: block; font-size: 11px; color: ${T.mut}; margin-top: 2px;">${protectedNote}</span>` : ''}
      </span>
    </span>
    <span style="flex: 0 0 128px; font-size: 12.5px; color: ${T.ink2};">${platform}</span>
    <span style="flex: 0 0 168px; font-size: 12.5px; color: ${T.ink2};">${projectRole}</span>
    <span style="flex: 0 0 300px; font-size: 12px; color: ${T.sec}; line-height: 1.4;">${perms}</span>
    <span style="flex: 0 0 120px;">${pill}</span>
    <span style="flex: 0 0 110px; font-size: 11.5px; color: ${T.mut};">${changed}</span>
    <span style="flex: 0 0 32px; display: flex; justify-content: flex-end; color: ${T.mut};">${L.icon('dots', 16)}</span>
  </div>`;

const permRow = (t, on) => `
  <div style="display: flex; align-items: center; gap: 10px; padding: 9px 0; border-top: 1px solid ${T.row};">
    <span style="width: 16px; height: 16px; border-radius: 4px; border: 1px solid ${on ? T.acc : T.lineStrong}; background: ${on ? T.acc : T.bg}; color: #FFFFFF; display: flex; align-items: center; justify-content: center;">${on ? L.icon('check', 11) : ''}</span>
    <span style="flex: 1 1 auto; font-size: 12.5px; color: ${on ? T.ink : T.sec};">${t}</span>
  </div>`;

// ============ UX-17 Участники и доступ ============
export const d17 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('members')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'Участники'],
    title: 'Участники и доступ',
    sub: 'Управляйте ролями и разрешениями внутри Проекта',
    actions: `<span style="display: inline-flex; padding: 3px; border-radius: 9px; background: ${T.field}; border: 1px solid ${T.line}; margin-right: 4px;">
        <span style="height: 30px; display: flex; align-items: center; padding: 0 14px; border-radius: 7px; font-size: 12.5px; color: ${T.sec};">Организация</span>
        <span style="height: 30px; display: flex; align-items: center; padding: 0 14px; border-radius: 7px; font-size: 12.5px; background: ${T.bg}; color: ${T.ink}; font-weight: 600; box-shadow: 0 1px 2px rgba(16,22,30,0.08);">Проект · Корпоративные продажи</span>
      </span><button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Добавить участника</button>`,
  })}

  <div style="flex: 1 1 auto; display: flex; gap: 18px; padding: 18px 24px 22px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden; display: flex; flex-direction: column; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 14px; padding: 11px 16px; background: ${T.field};">
        ${th('Участник', '1 1 0')}${th('Роль в платформе', '0 0 128px')}${th('Роль в Проекте', '0 0 168px')}${th('Основные разрешения', '0 0 300px')}${th('Статус', '0 0 120px')}${th('Изменено', '0 0 110px')}${th('', '0 0 32px')}
      </div>
      ${row('АВ', 'Анна Волкова', 'Владелец', 'Владелец Проекта', 'Полное управление Проектом, участниками и настройками',
        L.statusPill('done', 'Активна', 'sm'), 'сегодня', 'Последний владелец Проекта')}
      ${row('МО', 'Михаил Орлов', 'Оператор', 'Оператор', 'Может запускать работу, принимать решения и управлять ИИ-сотрудниками',
        L.statusPill('done', 'Активен', 'sm'), 'вчера', '')}
      ${row('ЕК', 'Елена Крылова', 'Участник', 'Участник', 'Может запускать работу и просматривать результаты',
        L.statusPill('done', 'Активна', 'sm'), '19 августа', '')}
      ${row('ИЛ', 'Игорь Лебедев', 'Аудитор', 'Аудитор', 'Только просмотр аудита и результатов, без изменений',
        L.statusPill('done', 'Активен', 'sm'), '18 августа', '')}
      <div style="flex: 1 1 auto;"></div>
      <div style="padding: 12px 16px; border-top: 1px solid ${T.row}; background: ${T.subtle}; display: flex; align-items: flex-start; gap: 10px;">
        <span style="color: ${T.warn}; display: flex; padding-top: 1px;">${L.icon('shield', 15)}</span>
        <span style="font-size: 12px; color: ${T.sec}; line-height: 1.5;">Последнего владельца Проекта нельзя удалить или понизить. Сначала назначьте другого владельца.</span>
      </div>
    </div>

    <aside style="flex: 0 0 388px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; display: flex; flex-direction: column; overflow: hidden;">
      <div style="padding: 14px 16px; border-bottom: 1px solid ${T.line};">
        <div style="font-size: 14.5px; font-weight: 600;">Добавить участника</div>
        <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 3px;">Пользователь выбирается из корпоративного каталога</div>
      </div>
      <div style="flex: 1 1 auto; padding: 14px 16px; display: flex; flex-direction: column; gap: 13px; min-height: 0;">
        ${L.field('Пользователь', 'Дмитрий Соловьёв · d.solovyov@company.ru')}
        <div>
          <div style="font-size: 11.5px; color: ${T.sec}; margin-bottom: 7px;">Роль в Проекте</div>
          <div style="display: flex; flex-direction: column; gap: 8px;">
            <span style="display: flex; align-items: flex-start; gap: 10px; padding: 11px 12px; border-radius: 9px; border: 1.5px solid ${T.acc}; background: ${T.accTint};">
              <span style="width: 16px; height: 16px; flex: 0 0 16px; border-radius: 8px; border: 5px solid ${T.acc}; box-sizing: border-box; margin-top: 1px;"></span>
              <span><span style="display: block; font-size: 13px; font-weight: 600;">Оператор</span><span style="display: block; font-size: 11.5px; color: ${T.sec}; margin-top: 2px;">Запускает работу, принимает решения, управляет ИИ-сотрудниками</span></span>
            </span>
            <span style="display: flex; align-items: flex-start; gap: 10px; padding: 11px 12px; border-radius: 9px; border: 1px solid ${T.line};">
              <span style="width: 16px; height: 16px; flex: 0 0 16px; border-radius: 8px; border: 1px solid ${T.lineStrong}; box-sizing: border-box; margin-top: 1px;"></span>
              <span><span style="display: block; font-size: 13px; font-weight: 500;">Участник</span><span style="display: block; font-size: 11.5px; color: ${T.sec}; margin-top: 2px;">Запускает работу и просматривает результаты</span></span>
            </span>
          </div>
        </div>
        <div>
          <div style="font-size: 11.5px; color: ${T.sec};">Дополнительные разрешения</div>
          ${permRow('Создавать и настраивать ИИ-сотрудников', true)}
          ${permRow('Запускать работу', true)}
          ${permRow('Принимать решения', true)}
          ${permRow('Управлять файлами', false)}
          ${permRow('Просматривать аудит', false)}
        </div>
        <div style="padding: 11px 12px; border-radius: 9px; background: ${T.subtle}; border: 1px solid ${T.line};">
          <div style="font-size: 11.5px; color: ${T.sec};">Что сможет делать участник</div>
          <div style="font-size: 12.5px; color: ${T.ink2}; margin-top: 5px; line-height: 1.45;">Запускать ИИ-сотрудников и Процессы, принимать решения человека, создавать и изменять ИИ-сотрудников этого Проекта.</div>
        </div>
      </div>
      <div style="padding: 12px 16px 16px; border-top: 1px solid ${T.line}; display: flex; gap: 8px;">
        <button style="flex: 1 1 auto; height: 36px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13px; font-weight: 600;">Добавить</button>
        <button style="flex: 0 0 104px; height: 36px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 12.5px;">Отменить</button>
      </div>
    </aside>
  </div>`,
}));
