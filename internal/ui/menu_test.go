package ui

import (
	"math"
	"testing"
)

func newTestMenu() *Menu {
	return NewMenu([]MenuItem{
		{Label: "Открыть", Icon: "open_in_new", Shortcut: "Enter"},
		{Separator: true},
		{Label: "Переименовать", Disabled: true},
		{Label: "Удалить", Icon: "delete", Shortcut: "Del"},
		{Separator: true},
		{Label: "Свойства", Icon: "info"},
	})
}

// 3.5 — навигация стрелками: разделители и disabled пропускаются.
func TestMenuNavigation(t *testing.T) {
	m := newTestMenu()
	if m.Hover() != 0 {
		t.Fatalf("стартовое выделение = %d, want 0", m.Hover())
	}

	m.Move(1)
	if m.Hover() != 3 { // разделитель (1) и disabled (2) пропущены
		t.Fatalf("после Move(1) hover = %d, want 3", m.Hover())
	}

	m.Move(1)
	if m.Hover() != 5 {
		t.Fatalf("после Move(1) hover = %d, want 5", m.Hover())
	}

	// Обёртка по кольцу: с последнего живого — в начало.
	m.Move(1)
	if m.Hover() != 0 {
		t.Fatalf("кольцо: hover = %d, want 0", m.Hover())
	}

	m.Move(-1)
	if m.Hover() != 5 {
		t.Fatalf("обратное направление: hover = %d, want 5", m.Hover())
	}
}

// 3.5 — Enter возвращает выбранный пункт; Esc закрывает (стек).
func TestMenuActivation(t *testing.T) {
	s := &MenuStack{}
	if _, ok := s.Key(false, false, true, false); ok {
		t.Fatal("Enter на пустом стеке не должен давать действие")
	}

	s.Push(newTestMenu())
	s.Top().Move(1) // → «Удалить»

	label, open := s.Key(false, false, true, false)
	if !open || label != "Удалить" {
		t.Fatalf("Enter: label=%q open=%v", label, open)
	}

	if s.Top() == nil {
		t.Fatal("Enter не должен закрывать меню")
	}
	s.Key(false, false, false, true) // Esc
	if s.Top() != nil {
		t.Fatal("Esc должен закрыть меню")
	}
}

// Hover по координатам: разделители не выделяются, вне меню — -1.
func TestMenuHoverAt(t *testing.T) {
	m := newTestMenu()
	m.Open(100, 100, 1920, 1080)

	// Верхний пункт: Y меню + паддинг + половина высоты пункта.
	if got := m.HoverAt(m.Y + MenuPadding + ItemHeight/2); got != 0 {
		t.Fatalf("hover на первом пункте = %d, want 0", got)
	}
	// Точка разделителя — не меняет выделение.
	h := m.HoverAt(m.Y + MenuPadding + ItemHeight + SepHeight/2)
	if got := m.HoverAt(m.Y + MenuPadding + ItemHeight + ItemHeight); got != h {
		t.Fatalf("hover на разделе не должен меняться: %d vs %d", got, h)
	}
	// Вне меню.
	if got := m.HoverAt(m.Y + m.Height() + 100); got != -1 {
		t.Fatalf("hover вне меню = %d, want -1", got)
	}
}

// 3.8 — меню живёт в ЭКРАННЫХ координатах: флип у краёв экрана не зависит
// от зума холста (модель вообще не принимает zoom — контракт слоя).
func TestMenuScreenCoordsFlip(t *testing.T) {
	screenW, screenH := 1920.0, 1080.0

	// Открытие у правого нижнего угла: меню должно перевернуться внутрь.
	m := newTestMenu()
	m.Open(screenW-10, screenH-10, screenW, screenH)
	if right := m.X + m.W; right > screenW-ScreenMargin+0.01 {
		t.Fatalf("меню вылезает вправо: %v", right)
	}
	if bottom := m.Y + m.Height(); bottom > screenH-ScreenMargin+0.01 {
		t.Fatalf("меню вылезает вниз: %v", bottom)
	}

	// Контракт 1:1 — повторное открытие в тех же экранных координатах даёт
	// ту же геометрию при любом зуме холста (в модель zoom не входит).
	m1 := newTestMenu()
	m1.Open(500, 300, screenW, screenH)
	m2 := newTestMenu()
	m2.Open(500, 300, screenW, screenH)
	if m1.X != m2.X || m1.Y != m2.Y || m1.Height() != m2.Height() {
		t.Fatal("геометрия меню нестабильна")
	}
	if math.Abs(m1.Height()-float64(2*MenuPadding+4*ItemHeight+2*SepHeight)) > 0.01 {
		t.Fatalf("высота меню: %v", m1.Height())
	}
}

// Оверлей питания: пункты с иконками Material Symbols.
func TestPowerOverlay(t *testing.T) {
	m := PowerOverlay()
	if len(m.Items) != 5 {
		t.Fatalf("пунктов питания: %d, want 5", len(m.Items))
	}
	for _, it := range m.Items {
		if it.Icon == "" {
			t.Fatalf("пункт %q без иконки", it.Label)
		}
	}
}
