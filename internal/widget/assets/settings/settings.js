// The settings widget edits the shell configuration through the Go backend.
// Every panel collects a small object; the widget sends the merged result with
// shell.setConfig, and the shell applies it without a restart.
(function () {
  const tabsEl = document.getElementById('tabs');
  const panelEl = document.getElementById('panel');
  const statusEl = document.getElementById('status');

  const GROUPS = [
    ['widgets', 'Виджеты'], ['apps', 'Приложения'], ['games', 'Игры'], ['media', 'Медиа'],
    ['office', 'Офис'], ['documents', 'Документы'], ['archive', 'Архивы'],
    ['folders', 'Папки'], ['files', 'Файлы'],
  ];
  const GROUP_TITLES = Object.fromEntries(GROUPS);

  const LAYOUTS = [
    ['tiled', 'Плитка'], ['monocle', 'Монокле'], ['stack', 'Стопка'], ['float', 'Плавающая'],
  ];

  const PANELS = [
    {
      id: 'look',
      title: 'Оформление',
      hint: 'Цвета стартового экрана и плиток.',
      fields: [
        { key: 'grid_background', label: 'Фон', type: 'color' },
        { key: 'grid_tile', label: 'Плитка', type: 'color' },
        { key: 'grid_selection', label: 'Выделение', type: 'color' },
        { key: 'grid_accent', label: 'Акцент', type: 'color' },
        { key: 'grid_text', label: 'Текст', type: 'color' },
        { key: 'grid_muted', label: 'Приглушённый', type: 'color' },
      ],
    },
    {
      id: 'wallpaper',
      title: 'Обои',
      hint: 'Картинка, анимированный GIF или видео на весь экран.',
      fields: [
        { key: 'wallpaper_enabled', label: 'Показывать обои', type: 'check' },
        { key: 'wallpaper_image', label: 'Файл', type: 'text' },
        { key: 'wallpaper_fps', label: 'Кадров в секунду', type: 'number', min: 1, max: 60 },
        { key: 'wallpaper_dim', label: 'Затемнение (0–1)', type: 'number', min: 0, max: 1, step: 0.05 },
      ],
      media: true,
    },
    {
      id: 'tiles',
      title: 'Плитки',
      hint: 'Размер, отступы и поведение плиток стартового экрана.',
      fields: [
        { key: 'grid_widgets', label: 'Показывать живые виджеты', type: 'check' },
        { key: 'grid_large', label: 'Крупная плитка 2×2', type: 'check' },
        { key: 'grid_large_min', label: 'Крупная от', type: 'number', min: 1, max: 12 },
        { key: 'grid_min_cell', label: 'Минимальная плитка', type: 'number', min: 40, max: 400 },
        { key: 'grid_max_cell', label: 'Максимальный размер плитки', type: 'number', min: 60, max: 480 },
        { key: 'grid_gap', label: 'Отступ между плитками', type: 'number', min: 0, max: 40 },
        { key: 'grid_border', label: 'Поле экрана', type: 'number', min: 0, max: 120 },
        { key: 'grid_header_height', label: 'Высота заголовка группы', type: 'number', min: 0, max: 80 },
        { key: 'grid_live_fps', label: 'Частота живых плиток', type: 'number', min: 1, max: 30 },
        { key: 'grid_live_limit', label: 'Одновременно живых плиток', type: 'number', min: 1, max: 12 },
      ],
    },
    { id: 'groups', title: 'Группы', hint: 'Порядок и состав полос на стартовом экране.', groups: true },
    {
      id: 'windows',
      title: 'Окна',
      hint: 'Рамки окон и порядок их размещения.',
      fields: [
        { key: 'layout', label: 'Раскладка', type: 'select', options: LAYOUTS },
        { key: 'gap', label: 'Отступ между окнами', type: 'number', min: 0, max: 40 },
        { key: 'border', label: 'Рамка', type: 'number', min: 0, max: 12 },
        { key: 'title_bar', label: 'Заголовок', type: 'number', min: 0, max: 40 },
        { key: 'active_border', label: 'Рамка активного', type: 'color' },
        { key: 'inactive_border', label: 'Рамка неактивного', type: 'color' },
        { key: 'title_background', label: 'Фон заголовка', type: 'color' },
        { key: 'title_foreground', label: 'Текст заголовка', type: 'color' },
        { key: 'follow_mouse', label: 'Фокус за мышью', type: 'check' },
        { key: 'focus_new_windows', label: 'Фокусировать новые окна', type: 'check' },
      ],
    },
    {
      id: 'desktops',
      title: 'Рабочие столы',
      hint: 'Сколько столов и как они называются.',
      fields: [
        { key: 'desktops', label: 'Количество', type: 'number', min: 1, max: 32 },
      ],
      desktops: true,
    },
    { id: 'keys', title: 'Горячие клавиши', hint: 'Сочетание пишется как Mod4+s или Super+space.', keys: true },
    {
      id: 'session',
      title: 'Запуск',
      hint: 'Программы, которыми оболочка пользуется.',
      fields: [
        { key: 'terminal', label: 'Терминал', type: 'list' },
        { key: 'menu', label: 'Меню запуска', type: 'list' },
        { key: 'startup', label: 'При старте', type: 'list' },
      ],
    },
    { id: 'about', title: 'О системе', hint: 'Что сейчас запущено и как.', about: true },
  ];

  let current = PANELS[0];
  let config = {};
  let info = {};
  let media = [];
  let actions = [];
  let groups = null;

  function el(tag, attrs, children) {
    const node = document.createElement(tag);
    Object.entries(attrs || {}).forEach(([key, value]) => {
      if (value === undefined || value === null) return;
      if (key === 'text') node.textContent = value;
      else if (key === 'html') node.innerHTML = value;
      else if (key === 'value') node.value = value;
      else if (key === 'type') node.type = value;
      else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
      else node.setAttribute(key, value);
    });
    (children || []).forEach((child) => child && node.appendChild(child));
    return node;
  }

  function button(text, className, onClick) {
    const node = el('button', { text, className: className || 'ghost' });
    node.addEventListener('click', onClick);
    return node;
  }

  function renderTabs() {
    tabsEl.innerHTML = '';
    PANELS.forEach((panel) => {
      const node = button(panel.title, panel === current ? 'current' : '',
        () => { current = panel; renderPanel(); renderTabs(); });
      tabsEl.appendChild(node);
    });
  }

  function clamp(value, min, max) {
    if (min !== undefined && value < min) value = min;
    if (max !== undefined && value > max) value = max;
    return value;
  }

  function normaliseColor(value) {
    return typeof value === 'string' && /^#[0-9a-f]{6}$/i.test(value) ? value : '#1c1f26';
  }

  // field builds one control and writes straight into the config object, so
  // "Применить" always sends what the page shows.
  // The label names its control, so a click on the text reaches the field the
  // same way it does in a browser.
  function labelled(spec, input, className) {
    const id = 'f-' + spec.key;
    input.id = id;
    const label = el('label', { className: className, for: id, text: spec.label });
    return [label, input];
  }

  function field(spec) {
    const value = config[spec.key];
    if (spec.type === 'check') {
      const input = el('input', { type: 'checkbox' });
      input.checked = value === true;
      input.addEventListener('change', () => { config[spec.key] = input.checked; });
      return el('div', { className: 'field' }, [
        el('label', { className: 'check' }, [input, el('span', { text: spec.label })]),
      ]);
    }
    const wrap = el('div', { className: 'field' });
    if (spec.type === 'color') {
      const input = el('input', { type: 'color', value: normaliseColor(value) });
      input.addEventListener('input', () => { config[spec.key] = input.value; });
      wrap.append(...labelled(spec, input));
      return wrap;
    }
    if (spec.type === 'select') {
      const select = el('select');
      (spec.options || []).forEach(([id, title]) => {
        const option = el('option', { text: title, value: id });
        if (value === id) option.selected = true;
        select.appendChild(option);
      });
      select.addEventListener('change', () => { config[spec.key] = select.value; });
      wrap.append(...labelled(spec, select));
      return wrap;
    }
    if (spec.type === 'number') {
      const input = el('input', { type: 'number' });
      if (spec.min !== undefined) input.min = spec.min;
      if (spec.max !== undefined) input.max = spec.max;
      if (spec.step !== undefined) input.step = spec.step;
      input.value = value === undefined ? '' : value;
      input.addEventListener('input', () => {
        const parsed = parseFloat(input.value);
        if (isNaN(parsed)) return;
        config[spec.key] = clamp(parsed, spec.min, spec.max);
      });
      input.addEventListener('blur', () => { input.value = config[spec.key]; });
      wrap.append(...labelled(spec, input));
      return wrap;
    }
    const input = el('input', { type: 'text', value: value === undefined ? '' : value });
    input.addEventListener('input', () => { config[spec.key] = input.value; });
    wrap.append(...labelled(spec, input));
    return wrap;
  }

  // listField edits a setting that is an array of strings, one per line.
  function listField(spec) {
    const wrap = el('div', { className: 'field' });
    const area = el('textarea', { rows: 3 });
    area.value = (config[spec.key] || []).join('\n');
    Object.assign(area.style, {
      background: 'rgba(255,255,255,.06)', color: 'inherit', font: 'inherit', fontSize: '13px',
      border: '1px solid rgba(255,255,255,.12)', borderRadius: '8px', padding: '6px 9px',
      resize: 'vertical', width: '100%', boxSizing: 'border-box',
    });
    area.addEventListener('input', () => {
      config[spec.key] = area.value.split('\n').map((line) => line.trim()).filter(Boolean);
    });
    wrap.append(...labelled(spec, area));
    wrap.appendChild(el('div', { className: 'hint', text: 'По одной строке на программу.' }));
    return wrap;
  }

  function renderMedia() {
    const box = el('div', { className: 'field' }, [el('label', { text: 'Готовые файлы' })]);
    const list = el('div', { id: 'mediaList' });
    if (!media.length) {
      list.appendChild(el('span', { className: 'hint', text: 'в папках с медиа ничего не найдено' }));
    }
    media.slice(0, 24).forEach((item) => {
      list.appendChild(button(item.name, '', () => {
        config.wallpaper_image = item.path;
        config.wallpaper_enabled = true;
        renderPanel();
        status('файл выбран, нажмите «Применить»');
      }));
    });
    box.appendChild(list);
    return box;
  }

  // groupOrder is the order the bands are painted in, with the missing ones
  // appended so a fresh configuration still shows everything.
  function groupOrder() {
    const chosen = Array.isArray(config.grid_groups) ? config.grid_groups.slice() : [];
    GROUPS.forEach(([key]) => { if (!chosen.includes(key)) chosen.push(key); });
    return chosen;
  }

  function renderGroups() {
    const wrap = el('div', {});
    const order = groupOrder();
    const list = el('div', { className: 'binds' });
    order.forEach((key, index) => {
      const enabled = Array.isArray(config.grid_groups) && config.grid_groups.includes(key);
      const row = el('div', { className: 'bind' }, [
        el('span', { text: GROUP_TITLES[key] || key }),
      ]);
      const buttons = el('div', { className: 'row' });
      buttons.appendChild(button('↑', '', () => {
        if (index === 0) return;
        const next = order.slice();
        [next[index - 1], next[index]] = [next[index], next[index - 1]];
        config.grid_groups = next;
        renderPanel();
      }));
      buttons.appendChild(button('↓', '', () => {
        if (index === order.length - 1) return;
        const next = order.slice();
        [next[index + 1], next[index]] = [next[index], next[index + 1]];
        config.grid_groups = next;
        renderPanel();
      }));
      buttons.appendChild(button(enabled ? '−' : '+', '', () => {
        const current = Array.isArray(config.grid_groups) ? config.grid_groups : [];
        const next = enabled
          ? current.filter((k) => k !== key)
          : groupOrder().filter((k) => k === key || current.includes(k));
        config.grid_groups = next;
        renderPanel();
      }));
      row.appendChild(buttons);
      if (!enabled) row.style.opacity = '.45';
      list.appendChild(row);
    });
    wrap.appendChild(list);
    const shown = (Array.isArray(config.grid_groups) ? config.grid_groups : [])
      .map((key) => GROUP_TITLES[key] || key);
    wrap.appendChild(el('div', {
      className: 'hint',
      text: shown.length ? 'Полосы: ' + shown.join(' → ') : 'Полосы не выбраны — показывается всё подряд',
    }));
    wrap.appendChild(el('div', {
      className: 'hint',
      text: '«−» убирает полосу с экрана, «↑» и «↓» меняют порядок.',
    }));
    return wrap;
  }

  function renderDesktops() {
    const wrap = el('div', {});
    const count = Number(config.desktops) || 1;
    const names = (config.desktop_names || []).slice();
    const list = el('div', { className: 'binds' });
    for (let i = 0; i < count; i++) {
      const input = el('input', { type: 'text', value: names[i] || '' });
      Object.assign(input.style, {
        background: 'rgba(255,255,255,.06)', color: 'inherit', font: 'inherit', fontSize: '13px',
        border: '1px solid rgba(255,255,255,.12)', borderRadius: '8px', padding: '4px 8px',
        minWidth: '0', flex: '1',
      });
      input.addEventListener('input', () => {
        const next = (config.desktop_names || []).slice();
        next[i] = input.value;
        config.desktop_names = next;
      });
      list.appendChild(el('div', { className: 'bind' }, [
        el('span', { text: 'Стол ' + (i + 1) }), input,
      ]));
    }
    wrap.appendChild(list);
    wrap.appendChild(el('div', {
      className: 'hint',
      text: 'Пустое имя останется как «Стол N».',
    }));
    return wrap;
  }

  // bindingFor finds the combination an action is bound to.
  function bindingFor(action) {
    const binds = config.keybinds || {};
    const combo = Object.keys(binds).find((key) => binds[key] === action);
    return combo || '';
  }

  function setBinding(action, combo) {
    const binds = Object.assign({}, config.keybinds || {});
    Object.keys(binds).forEach((key) => {
      if (binds[key] === action && key !== combo) delete binds[key];
    });
    if (combo) binds[combo] = action;
    config.keybinds = binds;
  }

  // comboPattern is the same shape the shell accepts, so the page can point at
  // a typo before it is sent.
  const comboPattern = /^(?:(?:Mod[1-5]|Super|Ctrl|Control|Shift|Alt)\+)*[A-Za-z0-9_]+$/;

  function renderKeys() {
    const wrap = el('div', {});
    const byGroup = new Map();
    actions.forEach((action) => {
      if (!byGroup.has(action.group)) byGroup.set(action.group, []);
      byGroup.get(action.group).push(action);
    });
    byGroup.forEach((list, group) => {
      wrap.appendChild(el('div', { className: 'hint', text: group }));
      const box = el('div', { className: 'binds' });
      list.forEach((action) => {
        const combo = bindingFor(action.id);
        const input = el('input', { type: 'text', value: combo, placeholder: 'не назначено' });
        Object.assign(input.style, {
          background: 'rgba(255,255,255,.06)', color: 'inherit', font: 'inherit', fontSize: '12.5px',
          border: '1px solid rgba(255,255,255,.12)', borderRadius: '8px', padding: '4px 8px',
          minWidth: '0', flex: '1',
        });
        const check = () => {
          const value = input.value.trim();
          if (value && !comboPattern.test(value)) {
            input.style.borderColor = '#e06c6c';
            status('не похоже на сочетание клавиш: ' + value);
            return;
          }
          input.style.borderColor = '';
          const clash = Object.keys(config.keybinds || {})
            .find((key) => key === value && (config.keybinds || {})[key] !== action.id);
          if (clash) {
            input.style.borderColor = '#e0b46c';
            status('сочетание ' + value + ' уже занято: ' + (config.keybinds || {})[clash]);
            return;
          }
          setBinding(action.id, value);
          status(value ? action.title + ': ' + value : action.title + ': снято');
        };
        input.addEventListener('change', check);
        input.addEventListener('blur', check);
        box.appendChild(el('div', { className: 'bind' }, [
          el('span', { text: action.title }), input,
        ]));
      });
      wrap.appendChild(box);
    });
    // Bindings the shell knows nothing about are listed so they can be fixed.
    const known = new Set(actions.map((a) => a.id));
    const extra = Object.entries(config.keybinds || {})
      .filter(([, action]) => !known.has(action));
    if (extra.length) {
      wrap.appendChild(el('div', { className: 'hint', text: 'Свои сочетания' }));
      const box = el('div', { className: 'binds' });
      extra.forEach(([combo, action]) => {
        const input = el('input', { type: 'text', value: combo });
        Object.assign(input.style, {
          background: 'rgba(255,255,255,.06)', color: 'inherit', font: 'inherit', fontSize: '12.5px',
          border: '1px solid rgba(255,255,255,.12)', borderRadius: '8px', padding: '4px 8px',
          minWidth: '0', flex: '1',
        });
        input.addEventListener('change', () => {
          const binds = Object.assign({}, config.keybinds || {});
          delete binds[combo];
          const next = input.value.trim();
          if (next && comboPattern.test(next)) binds[next] = action;
          config.keybinds = binds;
          renderPanel();
        });
        box.appendChild(el('div', { className: 'bind' }, [
          el('span', { text: action }), input,
        ]));
      });
      wrap.appendChild(box);
    }
    return wrap;
  }

  function renderAbout() {
    const wrap = el('div', { className: 'grid' });
    const card = (title, rows) => {
      const node = el('div', { className: 'card' }, [el('div', { className: 'title', text: title })]);
      rows.forEach(([key, value]) => {
        if (value === undefined || value === '') return;
        node.appendChild(el('div', {
          className: 'hint',
          text: key ? key + ': ' + value : value,
        }));
      });
      return node;
    };
    wrap.appendChild(card('Оболочка', [
      ['версия', (info.name || 'Lythrum') + ' ' + (info.version || '')],
      ['работает', info.uptime || '—'],
      ['раскладка', info.layout || '—'],
      ['конфигурация', info.configPath || 'встроенная'],
    ]));
    wrap.appendChild(card('Экран', [
      ['столов', info.desktops + ' (активный ' + info.desktop + ')'],
      ['мониторов', (info.monitors || []).join(', ') || '—'],
      ['окон', info.clients],
    ]));
    wrap.appendChild(card('Обои и мини-приложения', [
      ['обои', info.wallpaperEnabled ? (info.wallpaper || 'включены, файл не задан') : 'выключены'],
      ['мини-приложений', info.widgets],
      ['веб-движок', info.webEngine || '—'],
      ['сочетаний клавиш', info.keybinds],
    ]));
    wrap.appendChild(card('Сессия', [
      ['тип', info.sessionType || '—'],
      ['logind', info.session || 'без systemd-logind'],
      ['', info.sessionNote || ''],
    ]));
    wrap.appendChild(card('Горячие клавиши', [
      ['стартовый экран', info.startScreen || '—'],
    ]));
    return wrap;
  }

  function renderPanel() {
    panelEl.innerHTML = '';
    panelEl.appendChild(el('h2', { text: current.title }));
    panelEl.appendChild(el('div', { className: 'hint', text: current.hint }));
    if (current.about) {
      panelEl.appendChild(renderAbout());
      return;
    }
    if (current.keys) {
      panelEl.appendChild(renderKeys());
      return;
    }
    if (current.groups) {
      panelEl.appendChild(renderGroups());
      return;
    }
    const grid = el('div', { className: 'grid' });
    (current.fields || []).forEach((spec) => {
      grid.appendChild(spec.type === 'list' ? listField(spec) : field(spec));
    });
    panelEl.appendChild(grid);
    if (current.desktops) panelEl.appendChild(renderDesktops());
    if (current.media) panelEl.appendChild(renderMedia());
  }

  function status(text) {
    statusEl.textContent = text;
  }

  function busy(on) {
    document.getElementById('apply').disabled = on;
    document.getElementById('reload').disabled = on;
  }

  async function load() {
    status('загружаю настройки…');
    try {
      const [cfg, meta, acts] = await Promise.all([
        window.lythrum.call('shell.config', {}),
        window.lythrum.call('shell.info', {}),
        window.lythrum.call('shell.actions', {}).catch(() => []),
      ]);
      config = cfg || {};
      info = meta || {};
      actions = acts || [];
      groups = config.grid_groups;
      renderTabs();
      renderPanel();
      status('готово');
      // The wallpaper list may take a while on a big disk, so it follows.
      window.lythrum.call('media.list', { limit: 40 })
        .then((files) => {
          media = (files && (files.photos || []).concat(files.videos || [])) || [];
          if (current === PANELS[1]) renderPanel();
        })
        .catch(() => {});
    } catch (err) {
      status('настройки недоступны: ' + err);
    }
  }

  document.getElementById('apply').addEventListener('click', async () => {
    busy(true);
    status('применяю…');
    try {
      const next = await window.lythrum.call('shell.setConfig', config);
      config = next || config;
      Object.assign(info, await window.lythrum.call('shell.info', {}).catch(() => ({})));
      renderTabs();
      renderPanel();
      status('применено');
    } catch (err) {
      status('не получилось применить: ' + err);
    } finally {
      busy(false);
    }
  });
  document.getElementById('reload').addEventListener('click', () => {
    busy(true);
    load().finally(() => busy(false));
  });

  load();
})();