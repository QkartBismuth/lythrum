package widgets

import (
	"encoding/json"
	"testing"

	"lythrum/internal/core"
)

func testWidget(perms ...Permission) *Widget {
	return New("file:///tmp/widget.html", core.Rect{X: 100, Y: 100, W: 400, H: 300}, perms...)
}

// 4.2 — чужой токен отклоняется, даже с верной командой и правами.
func TestRPCWrongToken(t *testing.T) {
	w := testWidget(PermNotify)
	var notifyCalled int
	h := NewHandler(w, &CompositorAPI{
		Notify: func(id, text string) { notifyCalled++ },
	})

	args, _ := json.Marshal(map[string]string{"text": "привет"})
	req := Request{Token: w.Token, Cmd: CmdNotify, Args: args}

	if resp := h.Handle(req); !resp.OK {
		t.Fatalf("своим токеном команда должна проходить: %v", resp.Err)
	}
	if notifyCalled != 1 {
		t.Fatalf("notify вызван %d раз, want 1", notifyCalled)
	}

	resp := h.Handle(Request{Token: "чужой-токен", Cmd: CmdNotify, Args: args})
	if resp.OK {
		t.Fatal("чужой токен должен быть отклонён")
	}
	if notifyCalled != 1 {
		t.Fatalf("чужой токен не должен исполнять команду: %d вызовов", notifyCalled)
	}
}

// 4.2 — закрытый белый список: неизвестная команда отклоняется явно.
func TestRPCWhitelist(t *testing.T) {
	w := testWidget(PermNotify, PermCanvasWrite, PermFSRead)
	h := NewHandler(w, &CompositorAPI{})

	for _, cmd := range []string{"exec", "spawn", "eval_js", "read_all_fs", ""} {
		resp := h.Handle(Request{Token: w.Token, Cmd: cmd})
		if resp.OK {
			t.Fatalf("команда %q не в белом списке — должна быть отклонена", cmd)
		}
	}
}

// 4.3 — нарушение прав: команда отклоняется без исполнения.
func TestRPCPermissions(t *testing.T) {
	// Виджет БЕЗ прав: даже ping проходит, всё остальное — нет.
	w := testWidget()
	var notified int
	h := NewHandler(w, &CompositorAPI{
		Notify: func(id, text string) { notified++ },
	})

	if resp := h.Handle(Request{Token: w.Token, Cmd: CmdPing}); !resp.OK {
		t.Fatalf("ping не требует прав: %v", resp.Err)
	}

	args, _ := json.Marshal(map[string]string{"text": "x"})
	resp := h.Handle(Request{Token: w.Token, Cmd: CmdNotify, Args: args})
	if resp.OK || resp.Err == "" {
		t.Fatal("notify без права notify должен быть отклонён")
	}
	if notified != 0 {
		t.Fatalf("хук notify не должен вызываться: %d", notified)
	}

	// Право есть у соседа — не у этого виджета: тоже отказ.
	w2 := testWidget(PermNotify)
	h2 := NewHandler(w2, &CompositorAPI{Notify: func(id, text string) { notified++ }})
	if resp := h2.Handle(Request{Token: w2.Token, Cmd: CmdNotify, Args: args}); !resp.OK {
		t.Fatalf("с правом notify команда проходит: %v", resp.Err)
	}
	if notified != 1 {
		t.Fatalf("notify вызван %d раз, want 1", notified)
	}
}

// Токены виджетов уникальны и не пусты.
func TestTokensUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok := New("file://x", core.Rect{}).Token
		if tok == "" || seen[tok] {
			t.Fatalf("токен пуст или дублируется: %q", tok)
		}
		seen[tok] = true
	}
}

// 4.1 — маршрутизация ввода: обратный зум, клик при zoom 0.5 в цель.
func TestHitAtReverseZoom(t *testing.T) {
	ws := []*Widget{
		New("file://a.html", core.Rect{X: 200, Y: 200, W: 400, H: 300}),
		New("file://b.html", core.Rect{X: 100, Y: 100, W: 200, H: 200}), // нижний
	}

	// Камера с зумом 0.5: фрустум 3840x2160 при вьюпорте 1920x1080.
	cam := core.Camera{X: 0, Y: 0, Zoom: 0.5}

	// Точка холста (400,300) видна на экране как (200,150) при зуме 0.5.
	// Внутри только верхнего виджета a: b кончается на 300.
	screen := core.Vec2{X: 200, Y: 150}
	w, local, ok := HitAt(ws, cam, 1920, 1080, screen)
	if !ok {
		t.Fatal("клик мимо виджетов при обратном зуме")
	}
	if w.Rect.X != 200 {
		t.Fatalf("попали не в верхний виджет: %v", w.Rect)
	}
	// Локальная точка: canvas(400,300) − pos(200,200) = (200,100).
	if local.X != 200 || local.Y != 100 {
		t.Fatalf("локальная точка: %+v, want (200,100)", local)
	}

	// Верхний виджет выигрывает при пересечении: точка (250,250) — внутри
	// обоих виджетов; b (второй в срезе) — верхний.
	ws = append(ws, ws[1])
	ws[2] = New("file://b.html", core.Rect{X: 100, Y: 100, W: 200, H: 200})
	w, _, _ = HitAt(ws, cam, 1920, 1080, core.Vec2{X: 125, Y: 125})
	if w.Rect.X != 100 {
		t.Fatalf("topmost не выиграл: %v", w.Rect)
	}

	// Мимо всех — miss.
	if _, _, ok := HitAt(ws, cam, 1920, 1080, core.Vec2{X: 1900, Y: 1000}); ok {
		t.Fatal("клик вне виджетов не должен попадать")
	}

	// Зум 2: точка холста (150,125) видна на экране как (300,250).
	// Попадает в нижний виджет b: local = (150−100, 125−100) = (50,25).
	cam2 := core.Camera{X: 0, Y: 0, Zoom: 2}
	w, local, ok = HitAt(ws, cam2, 1920, 1080, core.Vec2{X: 300, Y: 250})
	if !ok {
		t.Fatal("клик при зуме 2 мимо")
	}
	if w.Rect.X != 100 || local.X != 50 || local.Y != 25 {
		t.Fatalf("зум 2: %v %+v", w.Rect, local)
	}
}
