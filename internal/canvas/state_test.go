package canvas

import (
	"testing"

	"lythrum/internal/core"
	"lythrum/internal/session"
)

// Спринт 1 (интеграция уровня ядра): окна получают id по порядку.
func TestAddWindowIDs(t *testing.T) {
	s := New(1920, 1080)
	a := s.AddWindow("A", core.Rect{X: 0, Y: 0, W: 10, H: 10})
	b := s.AddWindow("B", core.Rect{X: 50, Y: 0, W: 10, H: 10})
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("ids = %d, %d; want 1, 2", a.ID, b.ID)
	}
	if s.Focus != a.ID {
		t.Fatalf("первое окно должно получить фокус, got %d", s.Focus)
	}
}

// Спринт 1: MoveWindow примагничивает к соседу встык.
func TestMoveWindowSnaps(t *testing.T) {
	s := New(1920, 1080)
	n := s.AddWindow("N", core.Rect{X: 100, Y: 0, W: 200, H: 200})
	m := s.AddWindow("M", core.Rect{X: 0, Y: 0, W: 50, H: 50})

	// Тянем M правым краем к левой кромке N (дельта -7 < порога).
	res, ok := s.MoveWindow(m.ID, core.Rect{X: 57, Y: 50, W: 50, H: 50})
	if !ok {
		t.Fatal("MoveWindow не нашёл окно")
	}
	if res.Pos.X != 50 || m.Rect.Right() != n.Rect.X {
		t.Fatalf("снап не встык: pos=%v right=%v", res.Pos, m.Rect.Right())
	}
	if len(s.Guides()) == 0 {
		t.Fatal("магнитные направляющие не выставлены")
	}

	s.EndDrag()
	if len(s.Guides()) != 0 {
		t.Fatal("EndDrag не погасил направляющие")
	}
}

// Спринт 1/5: куллинг — окно за пределами фрустума спит.
func TestCullingMarksSleeping(t *testing.T) {
	s := New(1920, 1080)
	s.AddWindow("visible", core.Rect{X: 100, Y: 100, W: 200, H: 100})
	far := s.AddWindow("far", core.Rect{X: 5000, Y: 5000, W: 100, H: 100})

	if got := s.ApplyCulling(); got != 1 {
		t.Fatalf("видимых окон = %d, want 1", got)
	}
	if far.Sleeping != true {
		t.Fatal("дальнее окно должно спать")
	}

	// Панорама к окну будит его и усыпляет первое.
	s.Cam = core.Camera{X: 4950, Y: 4950, Zoom: 1}
	if got := s.ApplyCulling(); got != 1 {
		t.Fatalf("после панорамы видимых = %d, want 1", got)
	}
}

// Спринт 5 (ранний вариант): сериализация сессии туда-обратно.
func TestSessionRoundTrip(t *testing.T) {
	s := New(1920, 1080)
	s.Cam = core.Camera{X: 42, Y: 24, Zoom: 1.5}
	s.AddWindow("Terminal", core.Rect{X: 10, Y: 20, W: 640, H: 400})
	w2 := s.AddWindow("Music", core.Rect{X: 700, Y: 20, W: 300, H: 300})
	w2.Minimized = true

	snap := s.ToSnapshot()
	restored := New(1920, 1080)
	Restore(restored, snap)

	if restored.Cam != s.Cam {
		t.Fatalf("камера не восстановилась: %+v", restored.Cam)
	}
	got := restored.Windows()
	if len(got) != 2 {
		t.Fatalf("окон восстановлено %d, want 2", len(got))
	}
	if got[0].Title != "Terminal" || got[1].Title != "Music" || !got[1].Minimized {
		t.Fatalf("окна восстановились неверно: %+v", got)
	}
}

// Спринт 5: SessionManager атомарно пишет и читает файл.
func TestSessionManagerAtomicSaveLoad(t *testing.T) {
	m := session.NewManager(t.TempDir() + "/session.json")

	if _, err := m.Load(); err == nil {
		t.Fatal("чтение отсутствующего файла должно давать ошибку")
	}

	snap := session.Snapshot{
		Camera: session.CameraSnapshot{X: 1, Y: 2, Zoom: 3},
		Windows: []session.WindowSnapshot{
			{ID: 1, Title: "A", X: 0, Y: 0, W: 10, H: 10},
		},
	}
	if err := m.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != session.CurrentVersion {
		t.Fatalf("version = %d, want %d", got.Version, session.CurrentVersion)
	}
	if len(got.Windows) != 1 || got.Windows[0].Title != "A" || got.Camera.Zoom != 3 {
		t.Fatalf("снапшот исказился: %+v", got)
	}
}
