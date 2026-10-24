package main

import "testing"

func TestValidName(t *testing.T) {
	ok := []string{"report.txt", "фото.png", "a", ".hidden", "..foo", "with space"}
	for _, name := range ok {
		if !validName(name) {
			t.Errorf("validName(%q) = false, want true", name)
		}
	}
	bad := []string{"", ".", "..", "a/b", "/tmp/x", "a\x00b", "../escape"}
	for _, name := range bad {
		if validName(name) {
			t.Errorf("validName(%q) = true, want false", name)
		}
	}
}
