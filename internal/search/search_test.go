package search

import "testing"

func TestMatchFindsSubsequences(t *testing.T) {
	cases := []struct {
		query, name string
		wantOK      bool
	}{
		{"gimp", "GNU Image Manipulation Program", true},
		{"rhythm", "Rhythmbox", true},
		{"mp4", "clip.mp4", true},
		{"xyz", "clip.mp4", false},
		{"", "anything", true},
	}
	for _, c := range cases {
		_, ok := Match(c.query, c.name)
		if ok != c.wantOK {
			t.Errorf("Match(%q, %q) ok = %v, want %v", c.query, c.name, ok, c.wantOK)
		}
	}
}

func TestMatchIsCaseInsensitive(t *testing.T) {
	a, _ := Match("GIMP", "gimp")
	b, _ := Match("gimp", "GIMP")
	if a != b {
		t.Errorf("scores differ by case: %d and %d", a, b)
	}
}

func TestMatchPrefersWordStarts(t *testing.T) {
	word, _ := Match("no", "notepad")
	inside, _ := Match("no", "anno")
	if word <= inside {
		t.Errorf("a word start must score higher: %d and %d", word, inside)
	}
}

func TestMatchPrefersShorterNames(t *testing.T) {
	short, _ := Match("ab", "ab")
	long, _ := Match("ab", "a very long name with ab inside")
	if short <= long {
		t.Errorf("the shorter name must score higher: %d and %d", short, long)
	}
}

func TestRankSortsByScore(t *testing.T) {
	items := []Item{
		{Name: "Firefox", Path: "/usr/bin/firefox", Kind: "app", KindRank: 20},
		{Name: "Files", Path: "/usr/bin/files", Kind: "app", KindRank: 20},
		{Name: "firefox.desktop", Path: "/home/u/Desktop/firefox.desktop", Kind: "file"},
		{Name: "notes.txt", Path: "/home/u/Desktop/notes.txt", Kind: "file"},
	}
	results := Rank("fire", items, 0)
	if len(results) == 0 {
		t.Fatal("nothing matched")
	}
	if results[0].Name != "Firefox" && results[0].Name != "firefox.desktop" {
		t.Errorf("the first result is %q", results[0].Name)
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Score < results[i].Score {
			t.Fatalf("results are not sorted: %v", results)
		}
	}
	for _, r := range results {
		if r.Name == "notes.txt" {
			t.Error("an unrelated file must not appear")
		}
	}
}

func TestRankRespectsLimit(t *testing.T) {
	var items []Item
	for _, name := range []string{"aa", "ab", "ac", "ad"} {
		items = append(items, Item{Name: name})
	}
	if got := Rank("a", items, 2); len(got) != 2 {
		t.Errorf("limit 2 returned %d results", len(got))
	}
}

func TestRankEmptyQueryListsEverything(t *testing.T) {
	items := []Item{{Name: "one"}, {Name: "two"}}
	if got := Rank("", items, 0); len(got) != 2 {
		t.Errorf("an empty query returned %d results", len(got))
	}
}

func TestHighlightedSplitsName(t *testing.T) {
	runs := Highlighted("clip.mp4", []int{0, 1})
	if len(runs) == 0 {
		t.Fatal("no runs")
	}
	if !runs[0].Hit || runs[0].Text != "cl" {
		t.Errorf("first run = %+v", runs[0])
	}
	if len(runs) > 1 && runs[1].Hit {
		t.Errorf("the rest must not be highlighted: %+v", runs[1])
	}

	var sb string
	for _, r := range Highlighted("clip.mp4", []int{0, 1}) {
		sb += r.Text
	}
	if sb != "clip.mp4" {
		t.Errorf("the runs do not rebuild the name: %q", sb)
	}
}

func TestHighlightedWithoutMatches(t *testing.T) {
	runs := Highlighted("clip", nil)
	if len(runs) != 1 || runs[0].Hit || runs[0].Text != "clip" {
		t.Errorf("runs = %+v", runs)
	}
}

func TestSubstringBeatsScattered(t *testing.T) {
	sub, _ := Match("clip", "clip.mp4")
	scattered, _ := Match("clip", "Hardware Locality Istopo")
	if sub <= scattered {
		t.Errorf("a substring must beat a scattered match: %d and %d", sub, scattered)
	}
}

func TestAcronymStillFindsItsApp(t *testing.T) {
	if _, ok := Match("gimp", "GNU Image Manipulation Program"); !ok {
		t.Error("an acronym must still find its launcher")
	}
}
