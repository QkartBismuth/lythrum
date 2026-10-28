// Текстовые диалоги (Спринт 7): поиск по окнам и запуск команд.
//
// Контракт: символы приходят событиями шины (раскладку учёл ввод-процессор);
// без открытого диалога ввод игнорируется — символы уходят клиентам.
// Владелец диалога — main loop, как и всего холста.
package kernel

import (
	"strings"

	"lythrum/internal/canvas"
	"lythrum/internal/core"
	"lythrum/internal/ui"
)

type dialogKind uint8

const (
	dialogSearch dialogKind = iota // поиск окон по подстроке
	dialogRun                      // запуск команды
)

// dialog — открытый текстовый диалог.
type dialog struct {
	kind    dialogKind
	text    ui.TextInput
	matches []*canvas.Window // поиск: живые совпадения; run: nil
	match   int              // индекс текущего совпадения (Up/Down)
}

// OpenSearch открывает поиск; открытый диалог/оверлей закрываются.
func (k *Kernel) OpenSearch() {
	k.overlay = nil
	k.dialog = &dialog{kind: dialogSearch}
	k.recomputeMatches()
}

// OpenRun открывает диалог запуска.
func (k *Kernel) OpenRun() {
	k.overlay = nil
	k.dialog = &dialog{kind: dialogRun}
}

// CloseDialog закрывает диалог.
func (k *Kernel) CloseDialog() { k.dialog = nil }

// HandleChar — символ в открытый диалог; без диалога — no-op.
func (k *Kernel) HandleChar(s string) {
	if k.dialog == nil {
		return
	}
	k.dialog.text.Type(s)
	if k.dialog.kind == dialogSearch {
		k.dialog.match = 0 // новый запрос — с первого совпадения
		k.recomputeMatches()
	}
}

// HandleBackspace — забой в открытом диалоге; без диалога — no-op.
func (k *Kernel) HandleBackspace() {
	if k.dialog == nil {
		return
	}
	if k.dialog.text.Backspace() && k.dialog.kind == dialogSearch {
		k.dialog.match = 0
		k.recomputeMatches()
	}
}

// recomputeMatches — живые окна, чей заголовок содержит буфер без учёта
// регистра; пустой буфер — все живые окна.
func (k *Kernel) recomputeMatches() {
	q := strings.ToLower(k.dialog.text.Value())
	k.dialog.matches = nil
	for _, w := range k.State.Windows() {
		if w.Minimized {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(w.Title), q) {
			k.dialog.matches = append(k.dialog.matches, w)
		}
	}
}

// CycleMatch — Up/Down по совпадениям поиска (кольцо).
func (k *Kernel) CycleMatch(dir int) {
	if k.dialog == nil || k.dialog.kind != dialogSearch || len(k.dialog.matches) == 0 {
		return
	}
	n := len(k.dialog.matches)
	k.dialog.match = ((k.dialog.match+dir)%n + n) % n
}

// ActivateDialog — Enter: запуск команды либо переход к найденному окну
// (фокус + плавный подлёт камеры). Диалог закрывается всегда.
func (k *Kernel) ActivateDialog() {
	if k.dialog == nil {
		return
	}
	switch k.dialog.kind {
	case dialogRun:
		cmd := strings.TrimSpace(k.dialog.text.Value())
		k.dialog = nil
		if cmd != "" && k.Launch != nil {
			go k.Launch(cmd)
		}
	case dialogSearch:
		w := k.pickMatch()
		k.dialog = nil
		if w == nil {
			return
		}
		k.State.Focus = w.ID
		k.tween = core.NewTween(
			core.Vec2{X: k.State.Cam.X, Y: k.State.Cam.Y},
			k.State.CenterCamPos(w),
			CameraTweenDuration,
		)
	}
}

// pickMatch — текущее совпадение (nil — пусто).
func (k *Kernel) pickMatch() *canvas.Window {
	if k.dialog == nil || len(k.dialog.matches) == 0 {
		return nil
	}
	return k.dialog.matches[k.dialog.match]
}
