// Watchdog — сторож зависаний (Спринт 6).
//
// Отказоустойчивость: Supervisor покрывает КРАХИ модулей (panic/error →
// backoff-рестарт), но зависание (deadlock, вечный блок) крах не даёт —
// горутина жива и ничего не делает. Watchdog следит за пульсом main loop:
// пока сердце бьётся (метка времени обновляется каждый тик), всё в порядке;
// отставание больше maxLag — onStall (аварийный выход: systemd/лаунчер
// перезапустит композитор, сессия сохранена последним автосейвом).
package module

import (
	"context"
	"time"
)

// Watchdog — модуль-сторож. beat возвращает время последнего пульса.
type Watchdog struct {
	beat    func() time.Time
	maxLag  time.Duration
	onStall func(lag time.Duration)
}

// NewWatchdog собирает сторожа: зависание дольше maxLag вызывает onStall
// (ровно один раз на эпизод; восстановление пульса возвращает норму).
func NewWatchdog(beat func() time.Time, maxLag time.Duration, onStall func(lag time.Duration)) *Watchdog {
	return &Watchdog{beat: beat, maxLag: maxLag, onStall: onStall}
}

// Name — module.Module.
func (w *Watchdog) Name() string { return "watchdog" }

// Run следит за пульсом до отмены контекста. Период проверки — maxLag/4
// (не чаще 10 мс): задержка обнаружения 1–2 периода, ложных срабатываний
// нет — каждое измерение честное отставание.
func (w *Watchdog) Run(ctx context.Context) error {
	tick := w.maxLag / 4
	if tick < 10*time.Millisecond {
		tick = 10 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()

	stalled := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			lag := time.Since(w.beat())
			if lag > w.maxLag {
				if !stalled {
					stalled = true
					w.onStall(lag)
				}
			} else {
				stalled = false
			}
		}
	}
}
