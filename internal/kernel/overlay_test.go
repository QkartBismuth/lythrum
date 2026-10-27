package kernel

import (
	"image"
	"strings"
	"testing"

	"lythrum/internal/core"
	"lythrum/internal/input"
	"lythrum/internal/module"
	"lythrum/internal/palette"
	"lythrum/internal/widgets"
)

// 6.5 — Control Center: стрелка вправо на «Громкость» увеличивает громкость
// через шину (шаг ±5), подпись пункта обновляется по возврату события.
func TestControlCenterAdjustsVolume(t *testing.T) {
	h := newHarness(t)
	h.k.Pub = h.bus

	vol := 75
	h.k.OnVolume = func(v int) { vol += v }
	h.k.ControlValues = func() (int, bool) { return vol, false }

	h.bus.Publish(module.Event{Type: module.EvOpenControlCenter})
	h.pipe() // панель открыта, hover на «Громкость»

	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirRight})
	h.pipe()

	if vol != 80 {
		t.Fatalf("громкость: %d, want 80 (шаг +5 через шину)", vol)
	}
	label := h.k.overlay.Top().Items[0].Label
	if !strings.Contains(label, "80%") {
		t.Fatalf("подпись не обновилась: %q", label)
	}

	// Влево — уменьшение; зажим снизу невозможен здесь, но проверяем шаг.
	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirLeft})
	h.pipe()
	if vol != 75 {
		t.Fatalf("громкость: %d, want 75", vol)
	}
}

// Mute — Enter на пункте «Звук»; акцент — цикл пресетов через шину.
func TestControlCenterMuteAndAccent(t *testing.T) {
	h := newHarness(t)
	h.k.Pub = h.bus

	muted := false
	h.k.OnMute = func() { muted = !muted }
	h.k.ControlValues = func() (int, bool) { return 75, muted }

	h.bus.Publish(module.Event{Type: module.EvOpenControlCenter})
	h.pipe()

	// Вниз к «Звук», Enter — mute переключился.
	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirDown})
	h.bus.Publish(module.Event{Type: module.EvMenuActivate})
	h.pipe()
	if !muted {
		t.Fatal("mute не переключился")
	}
	if label := h.k.overlay.Top().Items[1].Label; !strings.Contains(label, "ВЫКЛЮЧЕН") {
		t.Fatalf("подпись mute: %q", label)
	}

	// Акцент: первый пресет совпадает с дефолтом, Enter → второй.
	if label := h.k.overlay.Top().Items[3].Label; !strings.Contains(label, "Лиловый") {
		t.Fatalf("стартовый акцент: %q", label)
	}
	before := palette.OverlayAccent.CSS()
	// Вниз два раза (Порог → Акцент), Enter → следующий пресет.
	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirDown})
	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirDown})
	h.bus.Publish(module.Event{Type: module.EvMenuActivate})
	h.pipe()
	if palette.OverlayAccent.CSS() == before {
		t.Fatal("акцент не сменился")
	}
	if label := h.k.overlay.Top().Items[3].Label; !strings.Contains(label, "Морской") {
		t.Fatalf("подпись акцента: %q", label)
	}
}

// Esc закрывает оверлей; навигация без оверлея — no-op.
func TestMenuCloseAndNavWithoutOverlay(t *testing.T) {
	h := newHarness(t)

	// Без оверлея стрелки не должны паниковать и менять холст.
	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirUp})
	h.pipe()

	h.bus.Publish(module.Event{Type: module.EvOpenControlCenter})
	h.pipe()
	if h.k.overlay == nil {
		t.Fatal("панель не открылась")
	}

	// Полный путь: клавиша Esc → ввод → шина → ядро.
	h.ip.HandleKeyEvent(input.KeyEvent{Key: input.KeyEscape, State: input.Press})
	h.pipe()
	if h.k.overlay != nil {
		t.Fatal("Esc не закрыл оверлей")
	}

	// Повторный Esc — no-op.
	h.bus.Publish(module.Event{Type: module.EvMenuClose})
	h.pipe()
}

// Диспетчер задач: статусы модулей попадают в пункты (read-only).
func TestTaskManagerShowsStatuses(t *testing.T) {
	h := newHarness(t)
	h.k.Statuses = func() []module.Status {
		return []module.Status{
			{Name: "audio", Running: true, Restarts: 2},
			{Name: "widgets", Running: false, Restarts: 5, LastErr: "cdp: connection refused"},
		}
	}
	h.bus.Publish(module.Event{Type: module.EvTaskManager})
	h.pipe()

	items := h.k.overlay.Top().Items
	if len(items) != 2 {
		t.Fatalf("пунктов: %d, want 2", len(items))
	}
	if !strings.Contains(items[0].Label, "audio: работает, перезапусков 2") {
		t.Fatalf("статус audio: %q", items[0].Label)
	}
	if !strings.Contains(items[1].Label, "connection refused") {
		t.Fatalf("ошибка не попала в статус: %q", items[1].Label)
	}
}

// История буфера: cap 10, новые сверху, выбор записи — активный буфер.
func TestClipboardRingAndMenu(t *testing.T) {
	h := newHarness(t)

	for i := 1; i <= 12; i++ {
		h.bus.Publish(module.Event{Type: module.EvClipboardPush, Str: "строка-" + string(rune('А'+i-1))})
	}
	h.pipe()
	if len(h.k.clipHistory) != ClipboardHistoryCap {
		t.Fatalf("глубина истории: %d, want %d", len(h.k.clipHistory), ClipboardHistoryCap)
	}
	if h.k.clipHistory[0] != "строка-Л" {
		t.Fatalf("новая запись не сверху: %q", h.k.clipHistory[0])
	}

	// Открыть меню и выбрать верхнюю запись.
	h.bus.Publish(module.Event{Type: module.EvClipboardHistory})
	h.pipe()
	h.bus.Publish(module.Event{Type: module.EvMenuActivate})
	h.pipe()
	if h.k.Clipboard != "строка-Л" {
		t.Fatalf("активный буфер: %q", h.k.Clipboard)
	}
}

// Скриншот: кадр рендерится и уходит в хук с размером вьюпорта.
func TestScreenshotHook(t *testing.T) {
	h := newHarness(t)
	h.k.State.AddWindow("Окно", core.Rect{X: 50, Y: 50, W: 300, H: 200})

	var got image.Image
	h.k.Screenshot = func(img image.Image) { got = img }

	h.bus.Publish(module.Event{Type: module.EvScreenshot})
	h.pipe()
	if got == nil {
		t.Fatal("хук скриншота не вызван")
	}
	if got.Bounds() != image.Rect(0, 0, 1920, 1080) {
		t.Fatalf("размер кадра: %v", got.Bounds())
	}

	// EvSnip — тот же кадр (выбор области отложен до text-input).
	got = nil
	h.bus.Publish(module.Event{Type: module.EvSnip})
	h.pipe()
	if got == nil {
		t.Fatal("снип не отрендерился")
	}
}

// Питание: Enter на «Заблокировать» публикует блокировку сессии.
func TestPowerOverlayDispatch(t *testing.T) {
	h := newHarness(t)
	// Эффекты ядра ловим отдельным каналом: pipe() осушает h.bus,
	// а вот в k.Pub публикуется ровно то, что ядро «сказало».
	pub := &module.ChanPublisher{C: make(chan module.Event, 16)}
	h.k.Pub = pub

	h.bus.Publish(module.Event{Type: module.EvPowerOverlay})
	h.pipe()
	if h.k.overlay.Top().Tag != "power" {
		t.Fatalf("тег оверлея: %q", h.k.overlay.Top().Tag)
	}
	h.bus.Publish(module.Event{Type: module.EvMenuActivate})
	h.pipe()

	select {
	case ev := <-pub.C:
		if ev.Type != module.EvLockSession {
			t.Fatalf("событие: %q", ev.Type)
		}
	default:
		t.Fatal("действие блокировки не опубликовано")
	}
}

// Виджет-кадры в снимке: заголовок «виджет · <файл>».
func TestFrameInputWidgetTitles(t *testing.T) {
	h := newHarness(t)
	h.k.State.AddWidget(widgets.New("file:///home/u/часы.html",
		core.Rect{X: 0, Y: 0, W: 100, H: 100}))

	in := h.k.frameInput()
	if len(in.Widgets) != 1 || !strings.Contains(in.Widgets[0].Title, "часы.html") {
		t.Fatalf("заголовок виджета: %+v", in.Widgets)
	}
}
