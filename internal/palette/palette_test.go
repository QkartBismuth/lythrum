package palette

import "testing"

func TestHexParse(t *testing.T) {
	cases := []struct {
		in   string
		want Color
	}{
		{"#14151A", Color{0x14, 0x15, 0x1A, 0xFF}},
		{"14151A", Color{0x14, 0x15, 0x1A, 0xFF}},
		{"#0E0F14E6", Color{0x0E, 0x0F, 0x14, 0xE6}},
		{"#ffffff", Color{0xFF, 0xFF, 0xFF, 0xFF}},
	}
	for _, c := range cases {
		got, err := Hex(c.in)
		if err != nil {
			t.Fatalf("Hex(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("Hex(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestHexParseErrors(t *testing.T) {
	for _, s := range []string{"", "#", "#123", "#12345", "#GGHHII", "#1234567"} {
		if _, err := Hex(s); err == nil {
			t.Fatalf("Hex(%q) ожидалась ошибка", s)
		}
	}
}

// Стартовые переменные обязаны быть корректными — guard от ручного редактирования.
func TestPaletteVarsParsed(t *testing.T) {
	for name, c := range All() {
		back, err := Hex(c.CSS())
		if err != nil {
			t.Fatalf("%s: CSS round-trip сломан: %v", name, err)
		}
		if back != c {
			t.Fatalf("%s: round-trip изменил цвет", name)
		}
	}
}

func TestPaletteValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("дефолтная палитра не прошла валидацию: %v", err)
	}
}
