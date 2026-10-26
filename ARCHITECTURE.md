# Lythrum — Wayland-композитор с бесконечным 2D-холстом

> Архитектурный документ. Проект пишется с нуля на Go. Старый X11-прототип
> живёт только в истории git (до коммита `0ff9163`) и не является основой.

---

## 1. Видение

Lythrum — экспериментальный Wayland-композитор, где рабочий стол — это
**бесконечная 2D-плоскость**. Монитор — камера над плоскостью. Окна, HTML5
виджеты и карточки файлов живут в глобальных координатах холста; камера
панорамируется и зумится жестами. Поверх холста — фиксированный UI-слой
оверлеев (масштаб всегда 1:1 к экрану).

Дизайн-система: шрифт **Geologica**, иконки **Material Symbols**, звуковая
схема **Chrome OS Flex**, палитра — ручной файл-заглушка `palette.go`.

---

## 2. Технологический стек

| Область | Решение | Обоснование |
|---|---|---|
| Wayland-сервер | `github.com/rajveermalviya/go-wayland` (server codegen) за интерфейсом `DisplayBackend` | Зрелая Go-библиотека с server-side; интерфейс позволяет подменить реализацию |
| Клавиатура | `libxkbcommon` (cgo) + собственная машина раскладок | Caps Lock как переключатель Ru/En требует своей логики поверх xkb state |
| HTML5-виджеты | Процессный рендерер: headless Chromium по CDP (screencast → буферы кадров) за интерфейсом `WidgetRenderer`; слот под CEF | Крах браузера = крах процесса, а не композитора; честная изоляция |
| Медиа | `image/gif|png|jpeg` + FFmpeg за интерфейсом `VideoDecoder` (fallback — покадровый `exec ffmpeg`) | Тестируемость без FFmpeg в CI |
| Звук | `github.com/ebitengine/oto/v2` + WAV PCM / `github.com/jfreymuth/vorbis` (OGG) | Чистый Go, без cgo |
| Шрифты | `golang.org/x/image/font/opentype` (TTF/OTF Geologica) + атлас глифов | Без C-freetype |
| IPC виджетов | Unix domain socket + JSON-RPC 2.0, per-widget токен и белый список команд | Аудит прав; WebSocket — при необходимости сети |
| Конкурентность | Состояние холста принадлежит main loop; модули общаются **только каналами** | Нет локов на горячем пути; `sync.Mutex` — только для снапшотов настроек/статусов |

---

## 3. Модель потоков и отказоустойчивость

```
                    ┌────────────────────────────────────────────┐
                    │              MAIN LOOP (1 горутина)         │
   input-горутина   │  select:                                    │
   InputProcessor ──┼─▶ bus.C (события) ──▶ мутации CanvasState   │
   WaylandBackend ──┼─▶ bus.C              ApplyCulling()         │
   WidgetManager ───┼─▶ bus.C              Render(CanvasLayer)    │
   SessionManager ──┼─▶ bus.C              Render(OverlayLayer)   │
                    │  ticker.C (16 ms)                           │
                    └────────────────────────────────────────────┘
                                       ▲
                    Supervisor ─────────┴──────────────┐
                    • каждый модуль — своя горутина    │
                    • recover() внутри runOnce()       │
                    • err → backoff → рестарт          │
                    • статусы → Control Center         │
```

Правила изоляции:
1. **Единственный владелец** `canvas.State` — main loop. Мьютексы на окна/камеру не нужны.
2. Модуль падает → `Supervisor` пишет ошибку, ждёт экспоненциальный backoff, рестартит. Ядро живо.
3. `recover()` в `runOnce()` и в критических точках рендера: паника = ошибка модуля.
4. Ввод никогда не блокируется: `ChanPublisher.Publish` — неблокирующий (переполнение → сброс события).
5. Зависание детектится heartbeat'ом (`Healthy()`), watchdog — в Спринте 5.

---

## 4. Карта пакетов

```
lythrum/
├── cmd/lythrum/main.go        # точка сборки: шина, supervisor, main loop
├── internal/
│   ├── core/                  # математика: Vec2/Rect, Camera, Snap, Tile
│   ├── palette/               # палитра-заглушка (ручное редактирование)
│   ├── module/                # Module, Event-шина, Supervisor
│   ├── input/                 # хоткеи Windows/KDE, Caps Lock-раскладки
│   ├── render/                # frustum culling, слои Canvas/Overlay
│   ├── canvas/                # состояние холста (владелец — main loop)
│   ├── session/               # JSON-сериализация сессии
│   ├── server/   (S1+)        # DisplayBackend: wayland / headless
│   ├── wallpaper/ (S3)        # GIF/видео/шейдеры → кадры фона
│   ├── audio/    (S3)         # AudioManager, звуки Chrome OS Flex
│   ├── preview/  (S3)         # карточки Live Preview
│   ├── ui/       (S3)         # контекстные меню, поиск, power overlay
│   ├── widgets/  (S4)         # WidgetManager, CDP-рендерер, IPC
│   ├── dnd/      (S4)         # wl_data_device, сквозной drag-and-drop
│   └── control/  (S5)         # Control Center, hot-reload настроек
├── assets/{fonts,icons,sounds}
└── ARCHITECTURE.md
```

---

## 5. Спецификация подсистем (ключевые структуры)

```go
// core — математическое ядро (чистые функции, 100% юнит-покрытие)
type Camera struct{ X, Y, Zoom float64 }        // (X,Y) — левый верх вьюпорта
type Rect   struct{ X, Y, W, H float64 }        // глобальные координаты холста
type Guide  struct{ Vertical bool; At float64 } // магнитная линия

// canvas — состояние холста (без мьютексов: владелец main loop)
type State struct {
    Cam core.Camera
    ViewportW, ViewportH, SnapThresholdPx float64
    windows map[WID]*Window
    guides  []core.Guide // активные магнитные линии
    cards   map[string]*preview.Card // карточки Live Preview (S3)
}
type Window struct {
    ID WID; Title string; Rect core.Rect
    Minimized bool; Sleeping bool // Sleeping = остановка frame-events
}

// module — каркас изоляции
type Module interface { Name() string; Run(ctx context.Context) error }
type Event struct { Type EventType; Str string; Num float64; Vec core.Vec2; Dir core.Dir }
type Supervisor struct{ /* рестарт с backoff, recover(), статусы */ }

// input — маршрутизатор клавиатуры
type InputProcessor struct {
    hot map[Combo]Action        // таблица хоткеев (см. §8)
    layout *LayoutMachine       // Caps Lock → Ru/En
    metaDown, otherKey bool    // детектор «одиночного Meta»
}

// session — персистентность
type Snapshot struct {
    Version int; Camera CameraSnapshot; Windows []WindowSnapshot
    Cards []CardSnapshot // карточки Live Preview (S3)
}
type Manager struct{ path string } // атомарная запись: tmp + rename

// wallpaper (S3) — обои с изоляцией отказов:
// Manager.SetSource(path) → Provider(Gif|Video|Shader) в горутине;
// паника/ошибка → backoff-перезапуск; последний кадр — latest-wins под RWMutex.
// preview (S3) — карточки: OpenCard(path, rect) → Card{ID, Path, Kind, Title, Rect, Poster};
// постер видео — первый кадр через ffmpeg; живут в canvas.State и сессии.
// audio (S3) — AudioManager: очередь имён → кэш декодированных Clip (WAV/OGG)
// → SubprocessPlayer (paplay/pw-play/aplay); SoundHook — события ядра → схема
// Chrome OS Flex (click/error/notify/device/power).
// ui (S3) — Menu/MenuStack/PowerOverlay: модель оверлей-слоя в ЭКРАННЫХ
// пикселях 1:1 (зум холста не участвует), флип у краёв, навигация клавишами.

// Планируемые (Спринты 4–5):
// dnd.DNDManager     — Source, GhostPos, DropTarget, DataDevice
// widgets.Manager    — instances, WidgetRenderer(CDP|CEF), rpc.Server, Permissions
// control.ConfigManager — sync.RWMutex + подписчики hot-reload
```

---

## 6. Математическое ядро

Преобразования (камера — левый верхний угол вьюпорта):

```
screen = (canvas − cam) · zoom        canvas = screen / zoom + cam
frustum = { cam.X, cam.Y, vpW/zoom, vpH/zoom }
```

Snapping: для каждой пары кромок (L↔L, R↔L, L↔R, R↔R; аналогично по Y)
вычисляется дельта совмещения; выбирается минимальная |дельта| ≤ порога.
Порог задаётся в **экранных** пикселях и делится на zoom — «магнит» ощущается
одинаково на любом масштабе. Выбранные кромки рисуются как направляющие
(`Guide`), цвет — `palette.SnapGuideLine`.

Тайлинг `Meta+Стрелки`: раскладка в доли **фрустума** (видимого экрана),
не холста: Left/Right — половины, Up — maximize, Down — нижняя половина.

---

## 7. Палитра (`internal/palette/palette.go`)

Изолированный файл-заглушка: переменные `palette.Color`, заданные HEX-строками
для ручного редактирования. Покрытие: фон холста, границы окон (фокус/без),
контекстное меню (фон/hover/текст), оверлеи (фон/акцент/кнопка), магнитная
линия. Валидация — на старте (`palette.Validate()`), ошибки HEX = отказ запуска.

---

## 8. Ввод и хоткеи (Windows 11 / KDE Plasma)

| Сочетание | Действие | Событие шины |
|---|---|---|
| `Meta` (одиночное) | Сброс камеры (0, 0, zoom 1.0) | `camera.reset` |
| `Meta+S` | Поиск по файлам (Geologica + Material) | `ui.search` |
| `Power` | Оверлей питания | `ui.power` |
| `Meta+L` | Блокировка → SDDM через `os/exec` | `session.lock` |
| `Caps Lock` | Переключение Ru/En (перехват, не фиксатор) | `input.layout` |
| `Meta+Пробел` | Переключение Ru/En | `input.layout` |
| Fn: XF86Audio/Brightness | Громкость ±5, mute, яркость ±5 | `audio.*`, `screen.*` |
| `Meta+E` / `Meta+I` / `Meta+R` | Проводник / Control Center / Run | `app.launch` / `ui.*` |
| `Meta+D` | Свернуть все окна | `ui.minimize_all` |
| `Ctrl+Shift+Esc` | Диспетчер задач | `ui.task_manager` |
| `Alt+F4` | Закрыть окно | `ui.close_window` |
| `Meta+V` | История буфера обмена | `ui.clipboard` |
| `PrintScreen` / `Meta+Shift+S` | Скриншот / ножницы | `ui.screenshot` / `ui.snip` |
| `Meta+Стрелки` | Тайлинг в доли видимого экрана | `ui.tile` |
| `Meta/Alt+Tab` | Фокус + плавное центрирование камеры | `ui.cycle_focus` |

Детектор одиночного Meta: press запоминает `metaDown`; любая другая клавиша
ставит `otherKey`; release без `otherKey` → сброс камеры. Автоповтор — игнор.

---

## 9. Слои и оптимизация рендера

- **LayerCanvas** — окна, виджеты, карточки: зумятся и панорамируются камерой.
- **LayerOverlay** — меню, поиск, power overlay: всегда 1:1 к экрану,
  рисуется после холста.

**Frustum culling** (`render.VisibleRects`): рендерятся и получают frame-events
только поверхности, пересекающие фрустум. Остальные — `Sleeping` (остановка
кадров, эконом CPU). `canvas.State.ApplyCulling()` — каждый тик main loop.

---

## 10. Дорожная карта (5 спринтов)

Методология каждой фичи: разработка → локальный коммит → юнит-тест модуля →
фиксы → интеграционный тест со стабильным ядром → финальный коммит.

### Спринт 1 — Базовое ядро ✅
Задачи: математика (камера/снап/тайлинг), палитра, каркас Module/Supervisor,
состояние холста, main loop, JSON-сессия (ранняя, для отладки).

| # | Уровень | Тест-кейс | Ожидание |
|---|---|---|---|
| 1.1 | Unit | `TestToScreenToCanvasRoundTrip` | обратное преобразование восстанавливает точку |
| 1.2 | Unit | `TestZoomAtKeepsPointUnderCursor` | точка под курсором неподвижна при зуме |
| 1.3 | Unit | `TestPanAccountsZoom` | пан на 100 экр. px при zoom 2 = 50 холст-px |
| 1.4 | Unit | `TestFrustum` | размер фрустума = viewport/zoom |
| 1.5 | Unit | `TestSnapRectDocksFlush` | кромки примагничиваются встык, направляющая создана |
| 1.6 | Unit | `TestSnapRectIgnoresBeyondThreshold` | за порогом — нет снапа и линий |
| 1.7 | Unit | `TestTileDirection` | Left/Right — половины, Up — maximize, Down — низ |
| 1.8 | Unit | `TestRectIntersects` | касание границ — не пересечение |
| 1.9 | Unit | `TestHexParse`, `TestPaletteValidate` | валидный/невалидный HEX, инварианты |
| 1.10 | Unit | `TestModuleCrashRecovery` | модуль паникует ×2 → 2 рестарта → работает |
| 1.11 | Unit | `TestSupervisorIsolation` | падение модуля не задевает соседей |
| 1.12 | Integration | main loop: события камеры → состояние | камера/холст сходятся, сессия пишется атомарно |

### Спринт 2 — Ввод, хоткеи, ресурсы ✅
Задачи: InputProcessor (готов), интеграция xkbcommon, загрузка Geologica,
атлас Material Symbols, WaylandBackend (seats, outputs).

| # | Уровень | Тест-кейс | Ожидание |
|---|---|---|---|
| 2.1 | Unit | `TestCapsLockTogglesLayout` | Caps Lock → `input.layout` us↔ru, событие поглощено |
| 2.2 | Unit | `TestSingleMetaResetsCamera` | press+release Meta → `camera.reset` |
| 2.3 | Unit | `TestMetaComboCancelsSingleMeta` | Meta+S+release → только `ui.search` |
| 2.4 | Unit | `TestFnKeysAndPower` | XF86 → volume/brightness; Power → overlay |
| 2.5 | Unit | `TestRepeatIgnored` | автоповтор не триггерит хоткеи |
| 2.6 | Unit | `TestMetaArrowsTile` | Meta+←/→/↑/↓ → `ui.tile` с верным Dir |
| 2.7 | Unit | загрузчик opentype: Geologica TTF/OTF | глифы читаются, атлас строится |
| 2.8 | Integration | headless-события → InputProcessor → main loop | full pipeline клавиатуры без дисплея |
| 2.9 | Integration | WaylandBackend: 2 клиента, фокус, геометрия | окна живут в глобальных координатах |

### Спринт 3 — Медиа, звук, контекст ✅ (текущий)
Задачи: WallpaperManager (GIF/видео/шейдер), AudioManager (WAV/OGG Chrome OS Flex),
карточки Live Preview, контекстные меню (оверлей-слой), жесты трекпада.

| # | Уровень | Тест-кейс | Ожидание |
|---|---|---|---|
| 3.1 | Unit | `TestGifProviderTimeline` | кадры по расписанию задержек метаданных GIF |
| 3.2 | Unit | `TestFFmpegVideoFrames` | декодер ffmpeg → RGBA-кадры под вьюпорт, темп по fps |
| 3.3 | Unit | `TestDecodeWAV`, `TestDecodeOGGAssets`, `TestManagerMissingFile` | WAV/OGG → сэмплы; нет файла — модуль жив |
| 3.4 | Unit | `TestKindOf`, `TestOpenCardImage` | image/gif/video/audio классифицируются, постер декодирован |
| 3.5 | Unit | `TestMenuNavigation`, `TestMenuActivation`, `TestMenuHoverAt` | навигация с пропуском разделителей, Esc закрывает |
| 3.6 | Integration | `TestManagerRecoversFromDecoderPanic` | паника декодера изолирована, backoff-перезапуск, метрика Restarts |
| 3.7 | Integration | `TestGesturePanSmoothing`, `TestGesturePinch` | EMA-сглаживание без выбросов, зум в точке |
| 3.8 | Integration | `TestMenuScreenCoordsFlip` | меню в экранных пикселях 1:1, флип у краёв, зум холста не участвует |

Дополнительно: карточки в `canvas.State` и сессии (`TestSessionRoundTrip`),
DND-дроп → карточка в ядре (`TestDroppedFileBecomesCard`), звуковой hook
(`TestSoundHookMapping`), звуковая схема Chrome OS Flex в `assets/sounds`.

### Спринт 4 — DND и HTML5-виджеты
Задачи: WidgetManager (CDP), проброс ввода с учётом зума, IPC-безопасность
(токены, белый список), wl_data_device DND, карточки из дропов.

| # | Уровень | Тест-кейс | Ожидание |
|---|---|---|---|
| 4.1 | Unit | координата курсора → canvas → CDP (обратный зум) | клик в виджет при zoom 0.5 попадает в цель |
| 4.2 | Unit | RPC: белый список команд, чужой токен | недопустимая команда отклоняется |
| 4.3 | Unit | Permissions: canvas_write/notify/fs_read | нарушение права → отказ |
| 4.4 | Unit | крах CDP-процесса | рестарт виджета, ядро живо |
| 4.5 | Unit | DND: источник→цель (Canvas/Card/Window) | тип данных сохраняется, дроп идемпотентен |
| 4.6 | Integration | дроп файла из окна на холст | создаётся карточка Live Preview |
| 4.7 | Integration | 2 виджета + окно, панорама холста | ввод роутится по фрустуму, сон виджетов работает |

### Спринт 5 — Оптимизация и конфигуратор
Задачи: culling + сон окон (frame-suspension), горячие настройки Control Center
(палитра, порог снапа, жесты, права виджетов), финализация сессий.

| # | Уровень | Тест-кейс | Ожидание |
|---|---|---|---|
| 5.1 | Unit | `VisibleRects` на 10k окон | только видимые индексы, O(n) |
| 5.2 | Unit | `ShouldSleep` | окно вне фрустума — сон |
| 5.3 | Unit | ConfigManager: запись настроек из другой горутины | гонок нет (-race), hot-reload доставлен |
| 5.4 | Unit | сессия: save/load round-trip + битый JSON | атомарность, устойчивость к мусору |
| 5.5 | Integration | 50 окон, панорама за пределы | CPU не растёт: спящие не рендерятся |
| 5.6 | Integration | смена палитры на лету | следующий кадр в новых цветах |
| 5.7 | Integration | kill -9 автосейва | файл сессии не повреждён (tmp+rename) |

---

## 11. Сборка и тесты

```bash
go build ./...        # без cgo: ядро собирается и тестируется в headless
go test ./... -race   # юнит-тесты всех спринтов
go run ./cmd/lythrum  # запуск ядра (headless main loop, Ctrl+C — сохранение сессии)
```

Интеграционные тесты с реальным Wayland (`internal/server`) подключаются
в Спринте 2 за build-тегом `wayland` и требуют `wayland-scanner` + `libxkbcommon`.
