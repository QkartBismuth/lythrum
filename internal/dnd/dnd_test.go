package dnd

import (
	"testing"

	"lythrum/internal/core"
)

// 4.5 — данные сохраняются по всему пути источник → цель.
func TestDragDropPreservesData(t *testing.T) {
	var m Manager
	payload := Payload{MIME: "text/uri-list", Data: []byte("file:///home/u/clip.mp4")}

	m.Start("widget-42", payload, core.Vec2{X: 10, Y: 20})
	if !m.Active() {
		t.Fatal("drag не начался")
	}

	// Данные доступны цели во время движения.
	m.Move(core.Vec2{X: 100, Y: 200})
	d := m.Drag_()
	if d == nil || d.Data.MIME != "text/uri-list" || string(d.Data.Data) != "file:///home/u/clip.mp4" {
		t.Fatalf("данные исказились: %+v", d)
	}
	if d.Pos.X != 100 || d.Pos.Y != 200 {
		t.Fatalf("позиция drag не обновилась: %+v", d.Pos)
	}

	// Дроп на карточку: данные доставлены 1:1.
	res, ok := m.Drop(Target{Kind: TargetCard, ID: "card-1", At: core.Vec2{X: 101, Y: 202}})
	if !ok {
		t.Fatal("дроп не удался")
	}
	if res.Data.MIME != payload.MIME || string(res.Data.Data) != string(payload.Data) {
		t.Fatalf("данные дропа исказились: %+v", res.Data)
	}
	if res.Target.Kind != TargetCard || res.Target.ID != "card-1" {
		t.Fatalf("цель исказилась: %+v", res.Target)
	}
	if m.Active() {
		t.Fatal("drag должен быть закрыт после дропа")
	}
}

// 4.5 — дроп идемпотентен: второй Drop без drag — отказ без паники.
func TestDropIdempotent(t *testing.T) {
	var m Manager

	// Дроп без начала drag — отказ.
	if _, ok := m.Drop(Target{Kind: TargetCanvas}); ok {
		t.Fatal("дроп без активного drag не должен удаваться")
	}
	if m.Drops() != 0 {
		t.Fatalf("счётчик дропов: %d, want 0", m.Drops())
	}

	// Один drag → ровно один успех; повтор — отказ.
	m.Start("win-1", Payload{MIME: "text/plain", Data: []byte("x")}, core.Vec2{})
	if _, ok := m.Drop(Target{Kind: TargetCanvas}); !ok {
		t.Fatal("первый дроп должен удаваться")
	}
	if _, ok := m.Drop(Target{Kind: TargetCanvas}); ok {
		t.Fatal("второй дроп того же drag — дубль, запрещён")
	}
	if m.Drops() != 1 {
		t.Fatalf("счётчик дропов: %d, want 1", m.Drops())
	}

	// Cancel идемпотентен и без drag.
	m.Cancel()
	m.Cancel()
	if m.Active() {
		t.Fatal("после cancel drag быть не должен")
	}
}

// Перезапуск drag во время перетаскивания — последний источник выигрывает.
func TestDragRestart(t *testing.T) {
	var m Manager
	m.Start("a", Payload{MIME: "text/plain", Data: []byte("1")}, core.Vec2{})
	m.Start("b", Payload{MIME: "text/plain", Data: []byte("2")}, core.Vec2{X: 5})

	res, ok := m.Drop(Target{Kind: TargetCanvas})
	if !ok {
		t.Fatal("дроп должен удаваться")
	}
	if string(res.Data.Data) != "2" {
		t.Fatalf("перезапуск drag: %+v", res.Data)
	}
	if m.Drag_() != nil {
		t.Fatal("после дропа активного drag быть не должно")
	}
}

// Заглушка Targeter для проверки контракта классификации.
type staticTargeter struct{ tgt Target }

func (s staticTargeter) TargetAt(core.Vec2) Target { return s.tgt }

// Targeter-контракт: классификация цели по точке.
func TestTargeterContract(t *testing.T) {
	var m Manager
	m.Start("w", Payload{MIME: "text/plain"}, core.Vec2{X: 1, Y: 2})

	tgt := Target{Kind: TargetWidget, ID: "wg-7", At: core.Vec2{X: 3, Y: 4}}
	res, ok := m.Drop(staticTargeter{tgt}.TargetAt(m.Drag_().Pos))
	if !ok || res.Target != tgt {
		t.Fatalf("targeter-контракт нарушен: %+v ok=%v", res.Target, ok)
	}
}
