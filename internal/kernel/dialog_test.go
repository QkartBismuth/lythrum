package kernel

import (
	"testing"
	"time"

	"lythrum/internal/core"
	"lythrum/internal/module"
)

// runDialog — открыть диалог запуска и набрать строку (полный путь:
// ввод → шина → ядро).
func runDialog(h *harness, text string) {
	h.bus.Publish(module.Event{Type: module.EvRunDialog})
	h.pipe()
	for _, r := range text {
		h.bus.Publish(module.Event{Type: module.EvChar, Str: string(r)})
	}
	h.pipe()
}

// 7.2 — диалог запуска: Enter передаёт команду в Launch и закрывает.
func TestRunDialogLaunch(t *testing.T) {
	h := newHarness(t)
	launches := make(chan string, 4)
	h.k.Launch = func(cmd string) { launches <- cmd }

	runDialog(h, "foot")
	if h.k.dialog == nil {
		t.Fatal("диалог не открылся")
	}

	h.bus.Publish(module.Event{Type: module.EvMenuActivate}) // Enter
	h.pipe()

	select {
	case cmd := <-launches:
		if cmd != "foot" {
			t.Fatalf("команда: %q, want foot", cmd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Launch не вызван")
	}
	if h.k.dialog != nil {
		t.Fatal("диалог не закрылся после Enter")
	}
}

// 7.2 — поиск: подстрока фильтрует окна, Enter фокусирует и летит камерой.
func TestSearchFocusesWindow(t *testing.T) {
	h := newHarness(t)
	term := h.k.State.AddWindow("Терминал", core.Rect{X: 0, Y: 0, W: 300, H: 200})
	h.k.State.AddWindow("Музыка", core.Rect{X: 400, Y: 0, W: 300, H: 200})
	h.k.State.AddWindow("Часы", core.Rect{X: 800, Y: 0, W: 300, H: 200})

	h.bus.Publish(module.Event{Type: module.EvToggleSearch})
	for _, r := range "тер" {
		h.bus.Publish(module.Event{Type: module.EvChar, Str: string(r)})
	}
	h.pipe()

	if h.k.dialog == nil || len(h.k.dialog.matches) != 1 {
		t.Fatalf("совпадений: %d, want 1 (Терминал)", len(h.k.dialog.matches))
	}
	h.bus.Publish(module.Event{Type: module.EvMenuActivate}) // Enter
	h.pipe()

	if h.k.State.Focus != term.ID {
		t.Fatalf("фокус: %d, want %d", h.k.State.Focus, term.ID)
	}
	if h.k.tween == nil {
		t.Fatal("камера не полетела к окну")
	}
	if h.k.dialog != nil {
		t.Fatal("диалог не закрылся")
	}
}

// Цикл совпадений Up/Down; повторное Meta+S закрывает поиск.
func TestSearchCycleAndToggle(t *testing.T) {
	h := newHarness(t)
	h.k.State.AddWindow("Музыка", core.Rect{X: 0, Y: 0, W: 300, H: 200})
	h.k.State.AddWindow("Погода", core.Rect{X: 400, Y: 0, W: 300, H: 200})

	h.bus.Publish(module.Event{Type: module.EvToggleSearch})
	for _, r := range "у" { // Музыка и (нет) Погода: одно совпадение
		h.bus.Publish(module.Event{Type: module.EvChar, Str: string(r)})
	}
	h.pipe()
	if len(h.k.dialog.matches) != 1 {
		t.Fatalf("совпадений: %d, want 1", len(h.k.dialog.matches))
	}

	// Пустой запрос → все живые окна; Down циклит.
	h.bus.Publish(module.Event{Type: module.EvBackspace})
	h.pipe()
	if len(h.k.dialog.matches) != 2 {
		t.Fatalf("совпадений при пустом запросе: %d, want 2", len(h.k.dialog.matches))
	}
	h.bus.Publish(module.Event{Type: module.EvMenuNav, Dir: core.DirDown})
	h.pipe()
	if h.k.dialog.match != 1 {
		t.Fatalf("индекс совпадения: %d, want 1", h.k.dialog.match)
	}

	h.bus.Publish(module.Event{Type: module.EvToggleSearch}) // повтор — закрыть
	h.pipe()
	if h.k.dialog != nil {
		t.Fatal("повторный Meta+S не закрыл поиск")
	}
}

// Esc закрывает диалог; ввод без диалога — no-op (клиентам, не ядру).
func TestDialogEscAndIgnoreWithoutDialog(t *testing.T) {
	h := newHarness(t)

	// Символы без диалога ничего не меняют и не паникуют.
	h.bus.Publish(module.Event{Type: module.EvChar, Str: "ы"})
	h.bus.Publish(module.Event{Type: module.EvBackspace})
	h.pipe()

	h.bus.Publish(module.Event{Type: module.EvRunDialog})
	h.pipe()
	h.bus.Publish(module.Event{Type: module.EvMenuClose}) // Esc
	h.pipe()
	if h.k.dialog != nil {
		t.Fatal("Esc не закрыл диалог")
	}
}

// Кадр с диалогом рендерится; строка содержит значение.
func TestDialogFrame(t *testing.T) {
	h := newHarness(t)
	h.k.Face = nil // без шрифта — панели без текста, не паникуем

	runDialog(h, "гидра")
	img := h.k.Frame()
	if img == nil {
		t.Fatal("кадр не отрендерился")
	}
	if h.k.dialog.text.Value() != "гидра" {
		t.Fatalf("буфер: %q", h.k.dialog.text.Value())
	}
}
