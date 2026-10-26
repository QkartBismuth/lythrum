//go:build wayland

package wayland

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Интеграционный тест 2.9 (lite): реальный клиент wayland-info подключается
// к нашему сокету и видит зарегистрированный wl_compositor.
// Запуск: go test -tags wayland ./internal/server/wayland/
func TestWaylandInfoConnects(t *testing.T) {
	if _, err := exec.LookPath("wayland-info"); err != nil {
		t.Skip("wayland-info не установлен")
	}

	w, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	// Дать дисплею подняться (сокет уже создан конструктором, но дать
	// event loop войти в работу).
	time.Sleep(200 * time.Millisecond)

	cmd := exec.Command("wayland-info")
	cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+w.SocketName())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wayland-info: %v:\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "wl_compositor") {
		t.Fatalf("wl_compositor не виден клиенту:\n%s", string(out))
	}
	t.Logf("wayland-info подключился к %q:\n%s", w.SocketName(), string(out))
}
