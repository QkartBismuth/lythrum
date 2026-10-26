package widgets

import (
	"encoding/json"
	"fmt"
)

// ── IPC: запросы виджета к композитору ──────────────────────────────────
//
// Виджеты НЕ доверенные: композитор проверяет каждый запрос в три шага:
//  1. токен — чужой виджет не может действовать от имени соседа;
//  2. белый список — неизвестная команда отклоняется явно;
//  3. права — команда требует конкретное Permission.
//
// Тест 4.2: белый список + чужой токен. Тест 4.3: нарушение прав → отказ.

// Request — запрос виджета.
type Request struct {
	Token string          `json:"token"`
	Cmd   string          `json:"cmd"`
	Args  json.RawMessage `json:"args,omitempty"`
}

// Response — ответ композитора.
type Response struct {
	OK   bool            `json:"ok"`
	Err  string          `json:"err,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Команды белого списка. Каждая требует право; отсутствие права — отказ.
const (
	CmdNotify   = "notify"    // PermNotify: показать уведомление
	CmdAddCard  = "add_card"  // PermCanvasWrite: создать карточку из файла
	CmdMoveCard = "move_card" // PermCanvasWrite: переместить карточку
	CmdReadFile = "read_file" // PermFSRead: прочитать файл (санитайзится)
	CmdSetClip  = "set_clip"  // PermCanvasWrite: положить в буфер обмена
	CmdPing     = "ping"      // без прав: health-check самого канала
)

// cmdSpec — спецификация команды белого списка.
type cmdSpec struct {
	perm Permission // требуемое право; "" — право не нужно
}

// whitelist — ЗАКРЫТЫЙ белый список: команда не из списка отклоняется,
// даже если права есть.
var whitelist = map[string]cmdSpec{
	CmdNotify:   {perm: PermNotify},
	CmdAddCard:  {perm: PermCanvasWrite},
	CmdMoveCard: {perm: PermCanvasWrite},
	CmdReadFile: {perm: PermFSRead},
	CmdSetClip:  {perm: PermCanvasWrite},
	CmdPing:     {perm: ""},
}

// CompositorAPI — исполнители команд на стороне композитора. Заполняется
// main loop'ом; nil-хук = команда недоступна (отказ, не паника).
type CompositorAPI struct {
	Notify   func(widgetID, text string)
	AddCard  func(widgetID, path string, at [2]float64) (cardID string)
	MoveCard func(widgetID, cardID string, to [2]float64) bool
	ReadFile func(widgetID, path string) ([]byte, error)
	SetClip  func(widgetID, text string)
}

// Handler — IPC-шлюз одного виджета: проверяет запросы и исполняет их
// через CompositorAPI. Иммутабелен после создания — потокобезопасен.
type Handler struct {
	widgetID string
	token    string
	perms    PermissionSet
	api      *CompositorAPI
}

// NewHandler строит шлюз виджета.
func NewHandler(w *Widget, api *CompositorAPI) *Handler {
	return &Handler{widgetID: w.ID, token: w.Token, perms: w.Perms, api: api}
}

// Handle — полный цикл проверки и исполнения запроса.
func (h *Handler) Handle(req Request) Response {
	// 1. Токен: чужой виджет не действует от имени этого.
	if req.Token != h.token {
		return errResp("токен отклонён")
	}

	// 2. Белый список: неизвестные команды отклоняются явно.
	spec, ok := whitelist[req.Cmd]
	if !ok {
		return errResp("команда не в белом списке")
	}

	// 3. Права: команда требует конкретное право.
	if spec.perm != "" && !h.perms.Allow(spec.perm) {
		return errResp(fmt.Sprintf("нет права %q для команды %q", spec.perm, req.Cmd))
	}

	// 4. Исполнение через композиторные хуки.
	if h.api == nil {
		return errResp("композитор недоступен")
	}
	switch req.Cmd {
	case CmdPing:
		return Response{OK: true, Data: json.RawMessage(`"pong"`)}
	case CmdNotify:
		var a struct{ Text string }
		if json.Unmarshal(req.Args, &a) != nil || a.Text == "" {
			return errResp("notify: нужен непустой text")
		}
		if h.api.Notify == nil {
			return errResp("notify недоступен")
		}
		h.api.Notify(h.widgetID, a.Text)
		return Response{OK: true}
	case CmdAddCard:
		var a struct {
			Path string     `json:"path"`
			At   [2]float64 `json:"at"`
		}
		if json.Unmarshal(req.Args, &a) != nil || a.Path == "" {
			return errResp("add_card: нужен path")
		}
		if h.api.AddCard == nil {
			return errResp("add_card недоступен")
		}
		id := h.api.AddCard(h.widgetID, a.Path, a.At)
		if id == "" {
			return errResp("add_card: файл не поддержан или не читается")
		}
		data, _ := json.Marshal(map[string]string{"id": id})
		return Response{OK: true, Data: data}
	case CmdMoveCard:
		var a struct {
			CardID string     `json:"card_id"`
			To     [2]float64 `json:"to"`
		}
		if json.Unmarshal(req.Args, &a) != nil {
			return errResp("move_card: нужны card_id и to")
		}
		if h.api.MoveCard == nil {
			return errResp("move_card недоступен")
		}
		if !h.api.MoveCard(h.widgetID, a.CardID, a.To) {
			return errResp("move_card: карточка не найдена")
		}
		return Response{OK: true}
	case CmdReadFile:
		var a struct {
			Path string `json:"path"`
			Max  int64  `json:"max,omitempty"`
		}
		if json.Unmarshal(req.Args, &a) != nil || a.Path == "" {
			return errResp("read_file: нужен path")
		}
		if h.api.ReadFile == nil {
			return errResp("read_file недоступен")
		}
		data, err := h.api.ReadFile(h.widgetID, a.Path)
		if err != nil {
			return errResp("read_file: " + err.Error())
		}
		raw, _ := json.Marshal(data)
		return Response{OK: true, Data: raw}
	case CmdSetClip:
		var a struct{ Text string }
		if json.Unmarshal(req.Args, &a) != nil {
			return errResp("set_clip: нужен text")
		}
		if h.api.SetClip == nil {
			return errResp("set_clip недоступен")
		}
		h.api.SetClip(h.widgetID, a.Text)
		return Response{OK: true}
	}
	return errResp("команда не в белом списке")
}

func errResp(msg string) Response { return Response{OK: false, Err: msg} }
