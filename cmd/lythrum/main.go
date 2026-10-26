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
	"os"
	"os/signal"
	"syscall"
	"time"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/input"
	"lythrum/internal/kernel"
	"lythrum/internal/module"
	"lythrum/internal/palette"
	"lythrum/internal/server"
	"lythrum/internal/session"
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

	// ── Ядро: применение событий к холсту ──
	k := kernel.New(state, sess)

	// ── Supervisor модулей ──
	sup := module.NewSupervisor(ctx)
	defer sup.Stop()

	// Дисплей — модуль: краш бэкенда не роняет ядро (Supervisor перезапустит).
	sup.Go(module.NewFunc("display.headless", backend.Run))

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
			return nil

		case ev := <-bus.C:
			if k.Apply(ev) {
				k.SaveSession()
				return nil
			}

		case <-ticker.C:
			k.Tick(16 * time.Millisecond.Seconds())
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
