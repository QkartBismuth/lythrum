# Сессия Lythrum

Файлы в `data/` добавляют Lythrum в список сессий дисплей-менеджера
(GDM, SDDM, LightDM, LXDM).

| файл                      | куда ставить                     | зачем                                                          |
|---------------------------|----------------------------------|----------------------------------------------------------------|
| `lythrum.desktop`          | `/usr/share/xsessions/`          | выбор Lythrum в списке сессий X11                             |
| `lythrum-wayland.desktop`  | `/usr/share/wayland-sessions/`   | запуск оболочки внутри Wayland-сессии через XWayland          |
| `lythrum.service`          | `/usr/share/dbus-1/services/`   | logind знает про оболочку и завершает её вместе с сеансом     |
| `lythrum.systemd`          | `/usr/lib/systemd/user/`        | модуль systemd, если D-Bus-файл установлен                   |

Файл `lythrum-wayland.desktop` запускает `lythrum --wayland`: оболочка
поднимается в уже работающей Wayland-сессии через XWayland и управляет
X11-окнами. Нативные Wayland-окна остаются под управлением композитора.

## Установка вручную

```bash
sudo install -Dm644 data/lythrum.desktop         /usr/share/xsessions/lythrum.desktop
sudo install -Dm644 data/lythrum-wayland.desktop /usr/share/wayland-sessions/lythrum.desktop
sudo install -Dm644 data/lythrum.service         /usr/share/dbus-1/services/lythrum.service
sudo install -Dm644 data/lythrum.systemd         /usr/lib/systemd/user/lythrum.service
```

Для одной сессии без прав root:

```bash
install -Dm644 data/lythrum.desktop ~/.config/xsessions/lythrum.desktop
```

Путь к бинарнику должен быть в `PATH` либо поправить `Exec=` в файле. После
установки новые сессии появляются в списке без перезагрузки; в некоторых
дисплей-менеджерах нужно перезапустить его.

## Проверка

```bash
ls /usr/share/xsessions/lythrum.desktop
lythrum --check                       # конфиг валиден
desktop-file-validate data/lythrum.desktop data/lythrum-wayland.desktop
```

## Особенности запуска

- **X11 обязателен.** На Wayland нужен XWayland, и тогда Lythrum запускают
  вручную внутри него либо через `startx`.
- **Дисплей-менеджер должен отдать корень.** Lythrum становится владельцем
  `_NET_SUPPORTING_WM_CHECK`; если корень уже занят, оболочка завершится с
  `wm: another window manager is already running on this display`. Для
  вложенного запуска внутри другой сессии нужен флаг `--replace`.
- **Переменные сессии.** `XDG_SESSION_ID` используется экраном питания для
  `loginctl terminate-session`; без него пункт «Завершить сеанс» просто
  закрывает оболочку.
- **Быстрый перезапуск.** `lythrum --cmd quit` освобождает сеанс, чтобы
  следующий `lythrum` стартовал без ошибки о занятом корне.

```bash
lythrum --cmd quit && lythrum          # перезапуск в терминале
lythrum --replace                      # вытеснить уже работающую оболочку
```