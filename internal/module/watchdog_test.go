package module

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Сторож зовёт onStall один раз при зависшем пульсе.
func TestWatchdogDetectsStall(t *testing.T) {
	var stale atomic.Int64 // пульс навсегда застрял в прошлом
	stale.Store(time.Now().Add(-time.Hour).UnixNano())

	calls := make(chan time.Duration, 4)
	w := NewWatchdog(
		func() time.Time { return time.Unix(0, stale.Load()) },
		20*time.Millisecond,
		func(lag time.Duration) { calls <- lag },
	)
	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)

	select {
	case lag := <-calls:
		if lag < 20*time.Millisecond {
			t.Fatalf("отставание подозрительно мало: %v", lag)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("onStall не вызван для зависшего пульса")
	}
	cancel()

	// Повторных вызовов быть не должно (один раз на эпизод).
	for {
		select {
		case <-calls:
			t.Fatal("onStall вызван повторно без восстановления пульса")
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

// Свежий пульс: сторож молчит. maxLag заведомо недостижим — детерминированно.
func TestWatchdogSilentOnFreshBeat(t *testing.T) {
	var beat atomic.Int64
	beat.Store(time.Now().UnixNano())

	calls := make(chan time.Duration, 4)
	w := NewWatchdog(
		func() time.Time { return time.Unix(0, beat.Load()) },
		time.Hour, // зависание невозможно
		func(lag time.Duration) { calls <- lag },
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Сердце продолжает биться ( сторож живёт с ним в одном тесте ).
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		beat.Store(time.Now().UnixNano())
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case lag := <-calls:
		t.Fatalf("ложное срабатывание: отставание %v", lag)
	case <-time.After(50 * time.Millisecond):
	}
}
