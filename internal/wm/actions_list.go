package wm

import "lythrum/internal/widget"

// shellActions lists everything a key binding may trigger. The settings page
// builds its editor from this list, so a new action shows up there without a
// second place to remember.
func shellActions() []widget.Action {
	actions := []widget.Action{
		{ID: "start_screen", Title: "Стартовый экран", Group: "Оболочка"},
		{ID: "search", Title: "Поиск", Group: "Оболочка"},
		{ID: "power", Title: "Питание и выход", Group: "Оболочка"},
		{ID: "logout", Title: "Завершить сеанс", Group: "Оболочка"},
		{ID: "reload", Title: "Перечитать настройки", Group: "Оболочка"},
		{ID: "restart", Title: "Перезапустить оболочку", Group: "Оболочка"},
		{ID: "quit", Title: "Выйти из оболочки", Group: "Оболочка"},

		{ID: "spawn", Title: "Открыть терминал", Group: "Окна"},
		{ID: "spawn_menu", Title: "Меню запуска", Group: "Окна"},
		{ID: "spawn_exec", Title: "Выполнить команду", Group: "Окна", NeedsArgument: true},
		{ID: "close_focused", Title: "Закрыть окно", Group: "Окна"},
		{ID: "focus_next", Title: "Фокус на следующее окно", Group: "Окна"},
		{ID: "focus_prev", Title: "Фокус на предыдущее окно", Group: "Окна"},
		{ID: "focus_left", Title: "Фокус влево", Group: "Окна"},
		{ID: "focus_right", Title: "Фокус вправо", Group: "Окна"},
		{ID: "focus_up", Title: "Фокус вверх", Group: "Окна"},
		{ID: "focus_down", Title: "Фокус вниз", Group: "Окна"},
		{ID: "toggle_float", Title: "Плавающее окно", Group: "Окна"},

		{ID: "tile", Title: "Плитка", Group: "Раскладка"},
		{ID: "monocle", Title: "Монокле", Group: "Раскладка"},
		{ID: "stack", Title: "Стопка", Group: "Раскладка"},
	}
	for i := 1; i <= 32; i++ {
		actions = append(actions, widget.Action{
			ID:    "desktop_" + itoa(i),
			Title: "Рабочий стол " + itoa(i),
			Group: "Столы",
		})
	}
	return actions
}

// itoa is a tiny helper so the action list stays readable.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
