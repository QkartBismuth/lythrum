package audio

import (
	"lythrum/internal/module"
)

// Имена звуков схемы Chrome OS Flex (файлы assets/sounds/<name>.ogg).
const (
	SoundClick  = "click"  // лёгкий тик: раскладка, сброс камеры
	SoundError  = "error"  // низкий двойной сигнал: ошибки действий
	SoundNotify = "notify" // мягкий колокол: уведомления, дроп файла
	SoundDevice = "device" // восходящий двойной: устройства, mute, питание
	SoundPower  = "power"  // нисходящий аккорд: завершение сессии
)

// SoundHook возвращает функцию побочных эффектов для kernel.Hook:
// отображает события шины на звуки схемы. Ядро остаётся независимым
// от аудио-подсистемы — main loop просто подключает этот хук.
//
// Вызывается из main loop ПОСЛЕ Apply: звук — эффект, а не изменение
// состояния, поэтому ошибок звука в холсте быть не может по построению.
func SoundHook(am *AudioManager) func(module.Event) {
	return func(ev module.Event) {
		if am == nil {
			return
		}
		switch ev.Type {
		case module.EvLayoutChanged, module.EvResetCamera:
			am.Play(SoundClick)
		case module.EvToggleSearch, module.EvOpenControlCenter:
			am.Play(SoundNotify)
		case module.EvNotify, module.EvDroppedFile:
			am.Play(SoundNotify) // «файл упал на холст»
		case module.EvPowerOverlay:
			am.Play(SoundDevice)
		case module.EvLockSession, module.EvVolumeMute:
			am.Play(SoundDevice)
		case module.EvQuit:
			am.Play(SoundPower)
		}
	}
}
