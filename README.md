# Lythrum

Тайлинговый оконный менеджер для X11 с экраном запуска в духе Windows 8:
живые HTML/CSS/JS-плитки, анимированные обои, превью файлов, поиск, экран
питания и приложение настроек.

Оболочка написана на Go и cgo поверх libX11, страницы плиток рисует
WebKitGTK 6.0 (GTK4) в offscreen-окне. Внешних Go-зависимостей нет.

```
┌── окна клиентов ──────────────────────────────────────────┐
│  [обои: картинка / GIF / видео]                            │
│    ┌───────────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐       │
│    │  часы     │ │CPU │ │наст│ │фото│ │муз │ │поиск│  ← живые
│    │  календарь│ │ RAM│ │ройк│ │    │ │    │ │    │    плитки
│    └───────────┘ └────┘ └────┘ └────┘ └────┘ └────┘       │
└────────────────────────────────────────────────────────────┘
```

## Что умеет

**Окна.** EWMH и ICCCM, тайлинг (`tiled`), `monocle`, `stack`, рамки с
заголовком, рабочие столы, фокус по мыши и по клавишам, перезапуск и выход.

**Бесконечный рабочий стол.** Рабочий стол — это канвас: он больше экрана и его
можно таскать мышью. Обои, иконки и окна живут в координатах канваса, поэтому
панорама двигает всё сразу, а окно можно оставить в стороне и доехать до него.
Тайлинг при этом заполняет ту часть канваса, которую видно сейчас.

**Ярлыки, файлы и папки.** То, что лежит в `~/Desktop`, нарисовано поверх
обоев: одиночный клик выбирает, двойной открывает. Иконки лежат под окнами,
поэтому maximized-окно закрывает их, а значки не мешают приложениям.

**Экран запуска.** Полосы групп (Виджеты, Приложения, Игры, Медиа, Офис,
Документы, Архивы, Папки, Файлы), крупная плитка 2×2, страницы, точки
страниц, мышь и клавиатура, превью фото, GIF и видео через ffmpeg.
Полосы наполняются из трёх источников: папка рабочего стола, установленные
`.desktop`-файлы (`~/.local/share/applications`, `/usr/share/applications`) и
файлы домашних папок (`~/Documents`, `~/Downloads`). Обход системных каталогов
идёт в фоне, поэтому экран открывается сразу. Страницы листаются колесом мыши
и перетаскиванием, `Page Up`/`Page Down` и точками внизу.

**Живые мини-приложения.** Шесть встроенных виджетов на HTML/CSS/JS, которые
рисуются в offscreen-окне и обновляются с заданной частотой:

| виджет   | что делает                                          | FPS |
|----------|-----------------------------------------------------|-----|
| clock    | часы, календарь, события из `.ics`                  | 4   |
| sysmon   | CPU, память, диски, сеть, температура, uptime       | 2   |
| settings | все настройки оболочки, применяются на лету          | 1   |
| photos   | слайдшоу фотографий и коротких видео                | 6   |
| music    | плеер с обложкой, эквалайзером и потоком из файла   | 6   |
| search   | подсказка про Super+S                               | 2   |

Клик по плитке открывает виджет на весь экран; ввод (клик, движение мыши,
клавиши) пересылается в страницу. Так как страница рисуется в невидимом
окне, оболочка запоминает элемент под последним кликом и правит его значение
напрямую: буквы вставляются по каретке, `BackSpace` удаляет символ, `Enter`
нажимает кнопку, а раскладка определяется по настоящему символу клавиши,
поэтому в поля можно вводить и кириллицу.

**Обои.** Статичная картинка, анимированный GIF или видео (ffmpeg) на весь
экран, с затемнением и настраиваемым FPS.

**Поиск.** Super+S — приложения и файлы с нечётким совпадением, стрелки,
Enter, Ctrl+U.

**Питание.** Super+L — завершение сеанса, сон, гибернация, перезагрузка,
выключение. Недоступные на этой машине пункты помечаются и не запускаются.

## Сборка

Нужны Go 1.24+, cgo и системные библиотеки:

```bash
# Arch / Fedora
sudo pacman -S --needed go gtk4 webkitgtk-6.0 libx11 libxft libxrandr \
    libxinerama libxext freetype2 fontconfig ffmpeg
```

Используются pkg-config-пакеты `x11 xft freetype2 fontconfig xrandr
xinerama xext` и `gtk4 webkitgtk-6.0`.

```bash
go build ./...
go test ./...                     # тесты, не требующие X-сервера
go install ./cmd/lythrum          # бинарник попадёт в $GOPATH/bin
```

## X11 и Wayland

Lythrum — оконный менеджер **для X11**. На Wayland он запускается внутри
**XWayland** и управляет только X11-окнами сессии.

| режим                | запуск                                              | что управляется                        |
|----------------------|-----------------------------------------------------|-----------------------------------------|
| X11-сессия           | `lythrum`                                           | все окна, включая нативные Wayland-приложения, отданные через XWayland |
| Wayland-сессия       | `lythrum --wayland` из `~/.config/wayland-sessions/` | только X11/XWayland-окна                |

Режим определяется сам: `XDG_SESSION_TYPE=wayland` или наличие
`WAYLAND_DISPLAY`. Флаг `--wayland` только подтверждает его. Текущий режим
виден в `lythrum --info` и в виджете настроек → «О системе» → «Сессия».

**Что работает внутри Wayland.** Тайлинг, рамки, рабочие столы, экран запуска
с живыми мини-приложениями, обои, превью, поиск, экран питания — всё это
рисование в X11, поэтому оно не зависит от композитора.

**Что не работает.** Нативные Wayland-окна (приложения, умеющие говорить
только с compositor, например Wayland-native Electron или GTK4-wayland)
компоновщик Wayland показывает сам: Lythrum их не видит, не двигает и не
управляет фокусом. Скриншоты (`import`) захватывают только X11-слой.

**Виджеты внутри Wayland.** WebKit выбирает бэкенд сам. Если нужно
принудительно увести поверхности виджетов в XWayland, поставь переменную:

```bash
LYTHRUM_WEBKIT_X11=1 lythrum
```

Мост выставляет `GDK_BACKEND=x11` только для GTK-потока виджетов. Обратная
ситуация тоже возможна: если снимки виджетов приходят пустыми, попробуйте
запустить сессию **без** этой переменной.

**Собственный Wayland-компоновщик** не входит в проект: для него нужен
wlroots или отдельный композитор на libwayland-server.

## Сессия в дисплей-менеджере

Готовый файл сессии лежит в [`data/`](data/README.md):

```bash
install -Dm644 data/lythrum.desktop ~/.config/xsessions/lythrum.desktop
# или для всех пользователей:
sudo install -Dm644 data/lythrum.desktop /usr/share/xsessions/lythrum.desktop
```

Для Wayland-сессии рядом ставится `data/lythrum-wayland.desktop` в
`~/.config/wayland-sessions/`: он запускает `lythrum --wayland` внутри
XWayland.

После этого Lythrum появляется в списке сессий GDM, SDDM, LightDM и LXDM.
Подробности, включая D-Bus-файл для logind, — в
[`data/README.md`](data/README.md).

## Запуск

```bash
lythrum --config ~/.config/lythrum/config.json
```

Флаги:

| флаг             | назначение                                        |
|------------------|---------------------------------------------------|
| `--display`      | X-дисплей (по умолчанию `$DISPLAY`)               |
| `--wayland`      | считать сессию Wayland (обычно определяется сам)   |
| `--config`       | путь к конфигу                                    |
| `--log`          | `debug`, `info`, `warn`, `error`                  |
| `--log-format`   | `text` или `json`                                 |
| `--replace`      | вытеснить уже запущенный менеджер                 |
| `--check`        | проверить конфиг и выйти                          |
| `--info`         | состояние запущенного менеджера                   |
| `--cmd "команда"` | отправить команду по IPC                          |

Конфиг ищется в `$LYTHRUM_CONFIG`, `$XDG_CONFIG_HOME/lythrum/config.json`,
`~/.config/lythrum/config.json`; без файла работают значения по умолчанию.

### Вложенный X-сервер для разработки

```bash
Xvfb :97 -screen 0 1280x800x24 &
DISPLAY=:97 go run ./cmd/lythrum --config /tmp/wall.json --log debug &
DISPLAY=:97 python3 - <<'EOF'
import socket, json, os
s = socket.socket(socket.AF_UNIX); s.connect(f"{os.environ['XDG_RUNTIME_DIR']}/lythrum.sock")
s.sendall(json.dumps({"command": "start_screen"}).encode())
print(s.recv(65536).decode())
EOF
DISPLAY=:97 import -window root /tmp/shot.png     # ImageMagick
```

Тесты, которым нужен живой дисплей, запускаются с `LYTHRUM_X11_TEST=1`.

## Управление

По умолчанию:

| сочетание      | действие                    |
|----------------|-----------------------------|
| `Mod4+space`   | экран запуска               |
| `Mod4+s`       | поиск                       |
| `Mod4+l`       | питание и выход             |
| `Mod4+Return`  | терминал                    |
| `Mod4+d`       | меню запуска                |
| `Mod4+j` / `k` | фокус вперёд / назад        |
| `Mod4+←` / `→`  | фокус влево / вправо        |
| `Mod4+q`       | закрыть окно                |
| `Mod4+Shift+q` | выйти из оболочки           |
| `Mod4+1..4`    | рабочий стол                |

Все сочетания и действия меняются в виджете «Настройки» → «Горячие клавиши»
или в `keybinds` конфига. Формат: `Mod4+s`, `Mod4+Shift+q`, `Super+space`;
действия — `start_screen`, `search`, `power`, `logout`, `reload`, `restart`,
`quit`, `spawn`, `spawn_menu`, `spawn_exec <команда>`, `close_focused`,
`focus_next`, `focus_prev`, `focus_left|right|up|down`, `toggle_float`,
`tile`, `monocle`, `stack`, `desktop_N`.

## Конфигурация

JSON; любое отсутствующее поле берётся из умолчаний. Проверка:
`lythrum --check`. Изменения из виджета настроек применяются без перезапуска
и проверяются до применения: неверный патч отклоняется целиком.

```jsonc
{
  "name": "Lythrum",
  "terminal": ["foot"],           // открывается на Mod4+Return
  "menu": ["wofi", "--dmenu"],
  "layout": "stack",              // stack | tiled | monocle | float
  "desktops": 4,
  "desktop_names": ["One", "Two", "Three", "Four"],

  "gap": 0, "border": 2, "title_bar": 18,
  "active_border": "#88c0d0", "inactive_border": "#4c566a",
  "title_background": "#2e3440", "title_foreground": "#eceff4",

  "grid_gap": 8, "grid_border": 24, "grid_aspect": 1.0,
  "grid_max_cell": 180, "grid_min_cell": 72,
  "grid_large": true, "grid_large_min": 3,
  "grid_header_height": 34,
  "grid_widgets": true,
  "grid_live_fps": 12, "grid_live_limit": 6,
  "grid_apps": true,          // установленные приложения в полосах
  "grid_home_files": true,    // файлы из ~/Documents и ~/Downloads
  "grid_file_limit": 400,     // больше этого файлов не собирать
  "desktop_icons": true,      // иконки рабочего стола поверх обоев
  "grid_groups": ["widgets", "apps", "games", "media", "office",
                  "documents", "archive", "folders", "files"],
  "grid_background": "#1c1f26", "grid_tile": "#2e3440",
  "grid_selection": "#434c5e", "grid_accent": "#88c0d0",
  "grid_text": "#eceff4", "grid_muted": "#8f9aad",

  "wallpaper_enabled": true,
  "wallpaper_image": "~/Pictures/background.mp4",
  "wallpaper_fps": 12,
  "wallpaper_dim": 0.0,           // 0–1

  "follow_mouse": true,
  "focus_new_windows": true,
  "keybinds": { "Mod4+s": "search" },
  "startup": ["nm-applet"]
}
```

## IPC

Сокет: `$XDG_RUNTIME_DIR/lythrum.sock`, иначе `/tmp/lythrum-<uid>.sock`.
Клиент — сам бинарник: `lythrum --cmd "start_screen"`.

| команда                     | что делает                                   |
|-----------------------------|----------------------------------------------|
| `ping`, `version`, `info`   | проверка связи, версия, состояние            |
| `list`, `focus`, `close`    | окна                                        |
| `spawn <команда>`           | запустить программу                          |
| `layout <имя>`, `desktop N` | раскладка, рабочий стол                      |
| `monitors`, `keybind`       | мониторы, сочетания                          |
| `search`, `power`           | открыть/закрыть панель поиска или питания    |
| `power <действие>`          | `logout`, `suspend`, `hibernate`, `reboot`, `poweroff` |
| `start_screen [open\|scan\|page]` | экран запуска                          |
| `desktop_entries`, `start_screen_info`, `start_screen_paint` | состояние экрана запуска |
| `desktop_scan`, `desktop_icons`                             | пересканировать плитки, перерисовать иконки |
| `canvas [сдвиг]` или `canvas X Y`                            | сдвинуть рабочий стол или показать точку     |
| `toggle_float`                                             | снять окно с тайлинга или вернуть обратно     |
| `reload`, `quit`            | перечитать конфиг, выйти                      |

Пример:

```bash
lythrum --cmd "power suspend"
lythrum --cmd "layout tiled"
lythrum --cmd start_screen
```

## Устройство кода

```
cmd/lythrum      оболочка: флаги, конфиг, сигналы, IPC-клиент
cmd/webdemo      ручная проверка offscreen-рендера WebKit
cmd/xdemo        окна X11 для проверки менеджера
cmd/xsnap        снимок корневого окна в PNG
cmd/wmprobe      проверка, что WebKit стартует в процессе оболочки
cmd/x11probe     минимальный пример cgo + libX11
cmd/uploadtest   нагрузка на загрузку картинок

data             файлы сессии: .desktop для дисплей-менеджера и D-Bus
                 описание для logind

internal/x11     тонкая обёртка над libX11: окна, события, текст Xft,
                 рисование, EWMH/ICCCM, клавиши
internal/config  конфиг: умолчания, проверка, частичное применение
internal/layout  чистая геометрия: раскладки, полосы групп, иконки стола
internal/desk    каталог плиток: .desktop-файлы, иконки, файлы, поиск
internal/imagex  загрузка и масштабирование иконок
internal/media   кадры из картинок, GIF и видео (ffmpeg)
internal/preview превью файлов: фото, GIF, видео, обложки
internal/search  нечёткий поиск по приложениям и файлам
internal/webview offscreen-рендер WebKitGTK: кадры, lythrum://, ввод
internal/widget  реестр мини-приложений и бэкенд (часы, система, медиа)
internal/wm      менеджер: события, клиенты, экран запуска, обои,
                 превью, поиск, питание, IPC
```

Правила, которые держат код в тонусе:

- `internal/layout`, `internal/config`, `internal/search`, `internal/media`
  не знают про X11 — всё, что можно, проверяется обычными тестами.
- Один цикл событий. GTK-поток и X-поток не смешивают: страницы получают
  команды через idle-колбэки, тяжёлая работа (ffmpeg, обход файлов,
  превью) уходит в горутины WM.
- Никаких внешних Go-модулей: стандартная библиотека и cgo.

## Мини-приложения

Виджет — это папка в `internal/widget/assets/<id>/` с `index.html` и
`common.js`, плюс запись в `builtin()` (`internal/widget/widget.go`).

API страницы:

```js
const config = await window.lythrum.call('shell.config', {});
await window.lythrum.call('shell.setConfig', { grid_gap: 12 });
const stats  = await window.lythrum.call('sysmon.stats', {});
const media  = await window.lythrum.call('media.list', { limit: 50 });
```

| метод            | ответ                                                |
|-------------------|------------------------------------------------------|
| `clock.now`       | время и дата                                        |
| `clock.calendar`  | календарь на месяц                                  |
| `clock.events`    | события из `.ics`                                    |
| `sysmon.stats`    | загрузка системы                                    |
| `shell.config`    | текущий конфиг                                      |
| `shell.setConfig` | применить часть конфига (проверяется целиком)       |
| `shell.theme`     | цвета оболочки                                      |
| `shell.info`      | версия, мониторы, uptime, оболочка                   |
| `shell.actions`   | список действий для редактора клавиш                 |
| `media.list`      | фото, видео и треки из домашних папок                |
| `media.folders`   | папки, доступные виджетам                            |

Файлы отдаются по адресу `lythrum://app/file/<base64 пути>`, поддерживаются
`Range` и `206 Partial Content` — поэтому в странице работают `<img>`,
`<video>` и `<audio>`.

## Ограничения

- Только X11. На Wayland нужен XWayland.
- Иконки рабочего стола нельзя перетаскивать: X11 не умеет альфа-композицию
  между окнами, поэтому каждая иконка рисуется на своей непрозрачной
  подложке, а слои рабочего стола в X11 не перетаскиваются.
- У канваса нет масштаба: только панорама. Обои при панораме повторяются
  плиткой, а не растягиваются.
- Положение канваса не запоминается между перезапусками оболочки.
- Живые плитки рисуются в offscreen-окне: анимации CSS в них не идут
  (страница помечается `html.offscreen`), а `<video>` без картинки-постера
  может остаться чёрным.
- Списки медиа ограничены по времени и глубине, чтобы обход домашней папки
  не подвешивал страницу.
- Виджет, открытый на весь экран, получает клавиши и клики через
  синтетические события: системные сочетания (Ctrl+C, Alt+Tab) в нём не
  работают, а переносы и автозаполнение браузера не воспроизводятся.
- `gdk_x11_surface_get_xid` и `gdk_x11_display_get_xdisplay` в C-мосте
  помечены в GTK4 как deprecated (сборка идёт с предупреждениями).