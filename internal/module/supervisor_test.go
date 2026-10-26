package module

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// flakyModule паникует первые два запуска, затем работает штатно до отмены.
type flakyModule struct{ calls atomic.Int32 }

func (f *flakyModule) Name() string { return "flaky" }
func (f *flakyModule) Run(ctx context.Context) error {
	n := f.calls.Add(1)
	if n <= 2 {
		panic("boom")
	}
	<-ctx.Done()
	return nil
}

// healthyModule просто живёт до отмены — «здоровый сосед».
type healthyModule struct{}

func (h *healthyModule) Name() string { return "healthy" }
func (h *healthyModule) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// failingModule всегда возвращает ошибку — для проверки бесконечного рестарта.
type failingModule struct{}

func (f *failingModule) Name() string { return "failing" }
func (f *failingModule) Run(ctx context.Context) error {
	return context.DeadlineExceeded
}

func eventually(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("условие не выполнилось за %v", d)
}

func statuses(sup *Supervisor) map[string]Status {
	m := make(map[string]Status)
	for _, st := range sup.Statuses() {
		m[st.Name] = st
	}
	return m
}

// TestModuleCrashRecovery — ядро отказоустойчивости: модуль, паникующий
// дважды, обязан пережить оба краха и вернуться в работу; ошибки падений —
// зафиксированы в статусах.
func TestModuleCrashRecovery(t *testing.T) {
	sup := NewSupervisor(context.Background())
	sup.SetBackoff(time.Millisecond, 5*time.Millisecond)
	defer sup.Stop()

	flaky := &flakyModule{}
	sup.Go(flaky)

	eventually(t, 3*time.Second, func() bool {
		st := statuses(sup)["flaky"]
		return st.Restarts >= 2 && st.Running
	})

	st := statuses(sup)["flaky"]
	if st.LastErr == "" {
		t.Fatalf("ошибки падений не зафиксированы: %+v", st)
	}
	if got := flaky.calls.Load(); got != 3 {
		t.Fatalf("модуль запускался %d раз, want 3 (2 краха + 1 успех)", got)
	}
}

// TestSupervisorIsolation — падение одного модуля не должно задевать соседей.
func TestSupervisorIsolation(t *testing.T) {
	sup := NewSupervisor(context.Background())
	sup.SetBackoff(time.Millisecond, 5*time.Millisecond)
	defer sup.Stop()

	sup.Go(&failingModule{})  // вечный падальщик
	sup.Go(&healthyModule{}) // свидетель

	eventually(t, 3*time.Second, func() bool {
		return statuses(sup)["failing"].Restarts >= 3
	})

	if st := statuses(sup)["healthy"]; !st.Running || st.Restarts != 0 {
		t.Fatalf("здоровый модуль пострадал от падения соседа: %+v", st)
	}
}

// TestSupervisorStop — Stop() завершает все горутины модулей.
func TestSupervisorStop(t *testing.T) {
	sup := NewSupervisor(context.Background())
	sup.SetBackoff(time.Millisecond, 5*time.Millisecond)

	sup.Go(&healthyModule{})
	sup.Stop()

	// wg.Wait внутри Stop не дал бы вернуться при зависшей горутине:
	// сюда дошли — значит, завершение детерминировано.
	if st := statuses(sup)["healthy"]; st.Running {
		t.Fatalf("модуль числится работающим после Stop: %+v", st)
	}
}
