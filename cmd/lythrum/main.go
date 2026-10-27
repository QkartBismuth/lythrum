// Command lythrum — ядро Wayland-композитора Lythrum.
//
// Точка сборки подсистем: DisplayBackend (headless / wayland), событийная
// шина, Supervisor, InputProcessor, Kernel (main loop логика), JSON-сессия.
// Ядро специально собирается и работает headless — полный цикл обработки
// событий гоняется в CI без дисплея; реальный Wayland-сервер включается
// сборкой с тегом wayland.
package main

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"lythrum/internal/audio"
	"lythrum/internal/canvas"
	"lythrum/internal/control"
	"lythrum/internal/core"
	"lythrum/internal/dnd"
	"lythrum/internal/input"
	"lythrum/internal/kernel"
	"lythrum/internal/module"
	"lythrum/internal/palette"
	"lythrum/internal/render"
	"lythrum/internal/server"
	"lythrum/internal/session"
	"lythrum/internal/wallpaper"
	"lythrum/internal/widgets"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("lythrum: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	// Палитра валидируется на старте: битый HEX = отказ запуска, а не
	// невидимый текст посреди сессии.
	if err := palette.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Дисплей ──
	// Головной вьюпорт; wayland-бэкенд (build tag wayland) заменит его
	// реальными output'ами Wayland.
	backend := server.NewHeadless(1920, 1080)
	out := backend.Outputs()[0]

	// ── Шина событий ──
	// Буфер 256: всплеск ввода (автоповтор + жесты) не должен теряться,
	// а Publish при переполнении всё равно неблокирующий.
	bus := &module.ChanPublisher{C: make(chan module.Event, 256)}

	// ── Холст: восстановление сессии ──
	state := canvas.New(float64(out.W), float64(out.H))
	sess := session.NewManager(sessionPath())
	if snap, err := sess.Load(); err == nil {
		canvas.Restore(state, snap)
	} else if !os.IsNotExist(err) {
		return err // битый JSON — диагностируемая проблема, не тихий пропуск
	}

	// ── Ввод: маршрутизатор хоткеев (горутина xkb подключается в wayland-бэкенде) ──
	_ = input.NewProcessor(bus, input.NewLayoutMachine())

	// ── Control Center: горячие настройки с hot-reload (Спринт 5) ──
	// Стартовые значения применяются к холсту до главного цикла; последующие
	// изменения UI Control Center'а едут через сеттеры (валидация → шина),
	// а применяет их только main loop (владелец холста и темы).
	cfgMgr := control.New()
	state.SnapThresholdPx = cfgMgr.Get().SnapThresholdPx

	// ── Ядро: применение событий к холсту ──
	k := kernel.New(state, sess)
	k.Pub = bus // оверлеи и акценты публикуют эффекты обратно в шину

	// ── Supervisor модулей ──
	sup := module.NewSupervisor(ctx)
	defer sup.Stop()

	// Watchdog: главный цикл бьёт сердце каждый тик; зависание дольше 5 с —
	// аварийный выход (лаунчер перезапустит; сессия — от последнего автосейва).
	var heart atomic.Int64
	heart.Store(time.Now().UnixNano())
	sup.Go(module.NewWatchdog(
		func() time.Time { return time.Unix(0, heart.Load()) },
		5*time.Second,
		func(lag time.Duration) {
			os.Stderr.WriteString(fmt.Sprintf(
				"lythrum: main loop завис на %v — аварийный выход\n", lag))
			os.Exit(3)
		},
	))

	// Дисплей — модуль: краш бэкенда не роняет ядро (Supervisor перезапустит).
	sup.Go(module.NewFunc("display.headless", backend.Run))

	// ── Обои: анимированный фон с изоляцией падений декодера ──
	// Источник задаётся переменной окружения (GIF/картинка/видео/shader);
	// паника декодера остаётся внутри Manager'а — холст не замечает.
	wall := wallpaper.NewManager(wallpaper.FFmpegDecoder{})
	if src := os.Getenv("LYTHRUM_WALLPAPER"); src != "" {
		wall.SetSource(src)
	}
	sup.Go(wall)

	// ── Звук: схема Chrome OS Flex, вывод в отдельный процесс ──
	// Нет аудио-стека (CI, сервер) → NullPlayer: логов нет, пан Compositor'а тоже.
	sounds := filepath.Join(appAssetsDir(), "sounds")
	am := audio.NewManager(sounds, audio.SubprocessPlayer{Kind: audio.AutoPlayer})
	sup.Go(am)

	// Побочные эффекты ядра: раскладка/питание/дроп → звук.
	k.Hook = audio.SoundHook(am)

	// ── Хуки ядра: шрифт, звук, статусы, скриншоты (Спринт 6) ──
	if face, err := render.LoadFontFile(render.DefaultFontPath(), 13); err == nil {
		k.Face = face
	} else {
		log.Printf("ui: шрифт не загружен (%v) — кадр без текста", err)
	}
	// Громкость: событие несёт ШАГ (Fn-клавиши и панель CC); применение —
	// AudioManager, снапшот — ConfigManager (Control Center показывает факт).
	k.OnVolume = func(step int) {
		am.SetVolume(am.Volume() + step)
		v := am.Volume()
		cfgMgr.Update(func(c *control.Config) { c.Volume = v })
	}
	k.OnMute = func() {
		m := am.ToggleMute()
		cfgMgr.Update(func(c *control.Config) { c.Muted = m })
	}
	k.ControlValues = func() (int, bool) { return am.Volume(), am.Muted() }
	k.Statuses = sup.Statuses // диспетчер задач: модули под надзором
	// Скриншоты: кадр рендерит ядро, запись — здесь (файловая система
	// не в ядре).
	k.Screenshot = saveShot

	// ── Виджеты: HTML5 в отдельных процессах (CDP) ──
	// Браузера нет → заглушки: композитор жив, виджеты рисуются плашками.
	widgetFactory := widgets.RendererFactory(widgets.CDPFactory{}.Build)
	if _, err := (widgets.CDPFactory{}).FindBrowser(); err != nil {
		log.Printf("widgets: браузер не найден (%v) — виджеты рисуются заглушками", err)
		widgetFactory = widgets.NullFactory
	}
	wm := widgets.NewManager(widgetFactory)
	sup.Go(wm)

	// ── DND: перетаскивание файлов и данных ──
	var dndMgr dnd.Manager

	// Автосейв: раз в 10 с и при завершении — событием в шину, чтобы запись
	// шла из main loop (владельца состояния), без гонок за снапшотом.
	sup.Go(module.NewFunc("session.autosave", func(ctx context.Context) error {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				bus.Publish(module.Event{Type: module.EvSessionSave})
				return nil
			case <-t.C:
				bus.Publish(module.Event{Type: module.EvSessionSave})
			}
		}
	}))

	// Демо-окна для ручной проверки холста (до подключения Wayland-клиентов).
	if len(state.Windows()) == 0 {
		state.AddWindow("Terminal", core.Rect{X: 120, Y: 120, W: 640, H: 400})
		state.AddWindow("Music", core.Rect{X: 800, Y: 160, W: 360, H: 240})
	}

	// ── MAIN LOOP ──
	// Единственный владелец canvas.State. Тик ~60 Гц: анимация камеры,
	// фрустум-куллинг (сон окон).
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			k.SaveSession()
			wm.Stop()
			return nil

		case ev := <-bus.C:
			if k.Apply(ev) {
				k.SaveSession()
				wm.Stop()
				return nil
			}
			// DND: дроп файла на холст → карточка уже в Apply; здесь
			// менеджер DND просто прощается с завершённым drag'ом.
			if ev.Type == module.EvDroppedFile {
				dndMgr.Drop(dnd.Target{Kind: dnd.TargetCanvas, At: ev.Vec})
			}

		case <-ticker.C:
			heart.Store(time.Now().UnixNano()) // пульс для watchdog'а
			k.Tick(16 * time.Millisecond.Seconds())

			// Синхронизация рендереров виджетов с состоянием холста:
			// запуск/останов по дельте + сон вне фрустума (тест 4.7).
			all := state.Widgets()
			wm.Sync(all)
			fr := state.Cam.Frustum(state.ViewportW, state.ViewportH)
			for _, wg := range all {
				wm.SetSleep(wg.ID, !fr.Intersects(wg.Rect))
			}
		}
	}
}

// sessionPath — расположение файла сессии (переопределяется для тестов).
func sessionPath() string {
	if p := os.Getenv("LYTHRUM_SESSION"); p != "" {
		return p
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return base + "/lythrum/session.json"
}

// saveShot пишет кадр скриншота в PNG. Каталог: LYTHRUM_SHOTS →
// ~/Pictures → системный tmp. Ошибка — лог, не паника: кадр не стоит
// сессии композитора.
func saveShot(img image.Image) {
	dir := os.Getenv("LYTHRUM_SHOTS")
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, "Pictures")
		} else {
			dir = os.TempDir()
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("screenshot: %v", err)
		return
	}
	name := filepath.Join(dir, "lythrum-"+time.Now().Format("2006-01-02-150405")+".png")
	f, err := os.Create(name)
	if err != nil {
		log.Printf("screenshot: %v", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Printf("screenshot: %v", err)
		return
	}
	log.Printf("screenshot: сохранён %s", name)
}

// appAssetsDir — каталог ресурсов (звуковая схема, шрифты). Порядок:
// 1) LYTHRUM_ASSETS; 2) рядом с бинарником (системная установка);
// 3) ./assets — запуск из корня репозитория (разработка).
func appAssetsDir() string {
	if p := os.Getenv("LYTHRUM_ASSETS"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "assets")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	if st, err := os.Stat("assets"); err == nil && st.IsDir() {
		return "assets"
	}
	return "assets" // консервативно: путь по умолчанию, ошибки — в логах audio
}
