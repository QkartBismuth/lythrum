package render

import (
	"testing"
)

// assetPath возвращает каталог ресурсов относительно каталога теста.
func assetPath(t *testing.T, rel string) string {
	t.Helper()
	// internal/render → корень модуля: ../../
	return "../../" + rel
}

// 2.7 — системный шрифт Geologica загружается и измеряется.
func TestLoadGeologica(t *testing.T) {
	f, err := LoadFontFile(assetPath(t, DefaultFontPath()), 16)
	if err != nil {
		t.Fatalf("Geologica: %v", err)
	}
	w, h := f.Measure("Lythrum")
	if w <= 0 || h <= 0 {
		t.Fatalf("измерение пустое: w=%v h=%v", w, h)
	}
	// Кириллица обязана поддерживаться (раскладка Ru).
	wRu, _ := f.Measure("Литрум")
	if wRu <= 0 {
		t.Fatal("кириллица не измеряется")
	}
}

// 2.7 — Material Symbols: карта иконок непуста, глифы присутствуют в шрифте.
func TestMaterialSymbols(t *testing.T) {
	f, err := LoadFontFile(assetPath(t, IconFontPath()), 24)
	if err != nil {
		t.Fatalf("Material Symbols: %v", err)
	}

	r, ok := IconGlyph("search")
	if !ok || r == 0 {
		t.Fatal("иконка search не найдена в карте кодпоинтов")
	}
	if !f.HasGlyph(r) {
		t.Fatalf("шрифт не содержит глиф U+%04X (search)", r)
	}

	for _, name := range []string{"power_settings_new", "settings", "close", "folder"} {
		if r, ok := IconGlyph(name); !ok || !f.HasGlyph(r) {
			t.Fatalf("иконка %q отсутствует в шрифте", name)
		}
	}

	if _, ok := IconGlyph("нет_такой_иконки"); ok {
		t.Fatal("несуществующее имя не должно находиться")
	}
}

// Битый шрифт — читаемая ошибка, а не паника.
func TestLoadFontBytesError(t *testing.T) {
	if _, err := LoadFontBytes([]byte("это не шрифт"), 16); err == nil {
		t.Fatal("ожидалась ошибка разбора")
	}
}
