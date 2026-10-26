//go:build wayland

// Package wayland — реальный серверный дисплей Lythrum на cgo
// libwayland-server.
//
// Почему cgo: чисто-Go серверных биндингов Wayland не существует
// (github.com/rajveermalviya/go-wayland поставляет только client-side).
// Сборка: go build -tags wayland ./...
//
// Обмен указателями с C: Go-указатели в C-памяти хранить НЕЛЬЗЯ (правила cgo),
// поэтому бэкенд регистрируется в реестре под int64-ID, и в C уходят только
// целые (приведённые к void* через intptr_t).
//
// Текущий охват (Спринт 2, итерация 1):
//   - создание wl_display и сокета (auto или именованного);
//   - глобальный объект wl_compositor с обработкой bind;
//   - приём клиентов (listener client_created);
//   - event loop wl_display_run в горутине + штатная остановка по ctx.
//
// Следующие итерации Спринта 2: wl_seat (клавиатура → InputProcessor через
// libxkbcommon), wl_output (реальная геометрия вместо заглушки), wl_surface/
// wl_shell → события surfaces в шину ядра.
package wayland

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"lythrum/internal/server"
)

/*
#cgo pkg-config: wayland-server
#include <stdlib.h>
#include <stdint.h>
#include <wayland-server-core.h>
#include <wayland-server-protocol.h>

// Go-коллбеки (реализованы //export-функциями ниже). Аргумент w — int64-ID
// бэкенда в Go-реестре, а не живой указатель. Указатели на структуры
// передаются как void*: cgo не экспортирует сигнатуры с *C.struct.
// Примечание: cgo генерирует прототипы с long long, поэтому extern'ы — тоже.
extern void lythrum_on_client_created(long long w, void *l, void *data);
extern void lythrum_bind_compositor(void *client, long long w, uint32_t version, uint32_t id);

// Шим под прототип wl_global_bind_func_t (void *data → int64-ID).
static void lythrum_bind_compositor_shim(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
	lythrum_bind_compositor(client, (int64_t)(intptr_t)data, version, id);
}

// Обёртка notify-коллбека: у wl_listener нет user data, поэтому слушатель
// вкладывается в структуру с ID бэкенда (классический container_of).
struct lythrum_client_listener {
	struct wl_listener listener;
	int64_t w;
};

static void lythrum_client_notify(struct wl_listener *l, void *data) {
	struct lythrum_client_listener *cl = (struct lythrum_client_listener *)l;
	lythrum_on_client_created(cl->w, l, data);
}

static struct lythrum_client_listener *lythrum_client_listener_new(int64_t w) {
	struct lythrum_client_listener *cl = calloc(1, sizeof(*cl));
	if (cl != NULL) {
		cl->w = w;
		cl->listener.notify = lythrum_client_notify;
	}
	return cl;
}

static const char *lythrum_add_socket_auto(struct wl_display *d) {
	return wl_display_add_socket_auto(d);
}

static int lythrum_add_socket_named(struct wl_display *d, const char *name) {
	return wl_display_add_socket(d, name);
}

static struct wl_global *lythrum_global_create_compositor(struct wl_display *d, int64_t w) {
	return wl_global_create(d, &wl_compositor_interface, 4,
	                        (void *)(intptr_t)w, lythrum_bind_compositor_shim);
}

static struct wl_resource *lythrum_compositor_resource(struct wl_client *c, int version, uint32_t id) {
	return wl_resource_create(c, &wl_compositor_interface, version, id);
}

static void lythrum_compositor_set_stub(struct wl_resource *r) {
	wl_resource_set_implementation(r, NULL, NULL, NULL);
}
*/
import "C"

// ── Реестр бэкендов: C-коллбеки приходят с int64-ID ─────────────────────

var registry = struct {
	mu sync.Mutex
	m  map[int64]*Wayland
}{m: make(map[int64]*Wayland)}

var registryNext = new(atomic.Int64)

func registerBackend(w *Wayland) int64 {
	id := registryNext.Add(1)
	registry.mu.Lock()
	registry.m[id] = w
	registry.mu.Unlock()
	return id
}

func backendByID(id int64) *Wayland {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.m[id]
}

func (w *Wayland) unregister() {
	registry.mu.Lock()
	delete(registry.m, w.id)
	registry.mu.Unlock()
}

// Wayland — Backend реального Wayland-сервера.
type Wayland struct {
	mu   sync.Mutex
	id   int64
	disp *C.struct_wl_display

	compositorGlobal *C.struct_wl_global

	// socketName — имя сокета (WAYLAND_DISPLAY) для клиентов.
	socketName string

	// clientListener — C-память слушателя подключений (живёт до destroy).
	clientListener *C.struct_lythrum_client_listener
}

// New создаёт дисплей и сокет. Пустой socketName → auto (wayland-0, wayland-1...).
func New(socketName string) (*Wayland, error) {
	disp := C.wl_display_create()
	if disp == nil {
		return nil, fmt.Errorf("wayland: wl_display_create failed")
	}
	w := &Wayland{disp: disp}
	w.id = registerBackend(w)

	// ── Сокет ──
	if socketName == "" {
		name := C.lythrum_add_socket_auto(disp)
		if name == nil {
			w.destroy()
			return nil, fmt.Errorf("wayland: wl_display_add_socket_auto failed")
		}
		w.socketName = C.GoString(name)
	} else {
		cname := C.CString(socketName)
		defer C.free(unsafe.Pointer(cname))
		if C.lythrum_add_socket_named(disp, cname) != 0 {
			w.destroy()
			return nil, fmt.Errorf("wayland: не удалось создать сокет %q", socketName)
		}
		w.socketName = socketName
	}

	// ── Глобальный wl_compositor (v4): фабрика wl_surface у клиентов ──
	w.compositorGlobal = C.lythrum_global_create_compositor(disp, C.int64_t(w.id))
	if w.compositorGlobal == nil {
		w.destroy()
		return nil, fmt.Errorf("wayland: wl_global_create(compositor) failed")
	}

	// ── Слушатель подключений клиентов ──
	w.clientListener = C.lythrum_client_listener_new(C.int64_t(w.id))
	if w.clientListener == nil {
		w.destroy()
		return nil, fmt.Errorf("wayland: alloc client listener failed")
	}
	C.wl_display_add_client_created_listener(disp, &w.clientListener.listener)

	return w, nil
}

// destroy освобождает дисплей (и снимает бэкенд с реестра).
func (w *Wayland) destroy() {
	w.unregister()
	C.wl_display_destroy(w.disp)
}

// Run крутит event loop дисплея до отмены контекста. Ошибка означает
// «дисплей упал» — Supervisor перезапустит бэкенд, ядро переживёт.
func (w *Wayland) Run(ctx context.Context) error {
	defer w.destroy()

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			C.wl_display_terminate(w.disp) // wl_display_run выйдет
		case <-stopped:
		}
	}()

	C.wl_display_run(w.disp)
	close(stopped)
	return ctx.Err()
}

// Outputs — геометрия дисплеев. Заглушка до регистрации wl_output (следующая
// итерация Спринта 2): реальные метаданные придут из событий ядра.
func (w *Wayland) Outputs() []server.OutputInfo {
	return []server.OutputInfo{{
		Name:  w.SocketName(),
		W:     1920,
		H:     1080,
		Scale: 1,
	}}
}

// SocketName — значение для WAYLAND_DISPLAY клиентов.
func (w *Wayland) SocketName() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.socketName
}

// ── C-коллбеки (вызываются из event loop libwayland) ────────────────────

//export lythrum_on_client_created
func lythrum_on_client_created(id C.longlong, listener unsafe.Pointer, data unsafe.Pointer) {
	_ = listener
	_ = data
	if w := backendByID(int64(id)); w != nil {
		// Обработка wl_client (PID, bind wl_surface) уйдёт в шину ядра на
		// следующей итерации; здесь — точка расширения.
	}
}

//export lythrum_bind_compositor
func lythrum_bind_compositor(clientPtr unsafe.Pointer, id C.longlong, version C.uint32_t, resID C.uint32_t) {
	if backendByID(int64(id)) == nil {
		return
	}
	client := (*C.struct_wl_client)(clientPtr)
	// Клиент запросил wl_compositor: создаём ресурс. Обработка get_surface
	// (создание wl_surface, события в шину ядра) — следующая итерация.
	ver := C.int(version)
	if ver > 4 {
		ver = 4
	}
	if res := C.lythrum_compositor_resource(client, ver, resID); res != nil {
		C.lythrum_compositor_set_stub(res)
	}
}
