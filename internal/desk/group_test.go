package desk

import (
	"reflect"
	"testing"
)

func TestGroupOfKinds(t *testing.T) {
	cases := []struct {
		entry Entry
		want  string
	}{
		{Entry{Kind: KindDir}, GroupFolders},
		{Entry{Kind: KindFile, Path: "/x/photo.jpg"}, GroupMedia},
		{Entry{Kind: KindFile, Path: "/x/paper.pdf"}, GroupDocs},
		{Entry{Kind: KindFile, Path: "/x/sheet.ods"}, GroupOffice},
		{Entry{Kind: KindFile, Path: "/x/pack.zip"}, GroupArchive},
		{Entry{Kind: KindFile, Path: "/x/unknown.xyzzy"}, GroupFiles},
		{Entry{Kind: KindFile, Path: "/x/noextension"}, GroupFiles},
		{Entry{Kind: KindApp}, GroupApps},
		{Entry{Kind: KindApp, Categories: "Game;"}, GroupGames},
		{Entry{Kind: KindApp, Categories: "AudioVideo;Player;"}, GroupMedia},
		{Entry{Kind: KindApp, Categories: "Office;Spreadsheet;"}, GroupOffice},
		// An editor is an application; "Office" outranks the utility tag.
		{Entry{Kind: KindApp, Categories: "Utility;TextEditor;"}, GroupApps},
		{Entry{Kind: KindApp, Categories: "Office;TextEditor;"}, GroupOffice},
		{Entry{Kind: KindShortcut}, GroupApps},
	}
	for _, c := range cases {
		if got := GroupOf(c.entry); got != c.want {
			t.Errorf("GroupOf(%+v) = %q, want %q", c.entry, got, c.want)
		}
	}
}

func TestGroupOfCategoriesFollowsTheFirstMatch(t *testing.T) {
	// The entry lists Game first, so that is the band it lands in.
	if got := GroupOf(Entry{Kind: KindApp, Categories: "Game; AudioVideo;"}); got != GroupGames {
		t.Errorf("GroupOf = %q, want games", got)
	}
	// A later category still wins when the earlier one is unknown.
	if got := GroupOf(Entry{Kind: KindApp, Categories: "Whatever; Game ;"}); got != GroupGames {
		t.Errorf("GroupOf = %q, want games", got)
	}
	if got := GroupOf(Entry{Kind: KindApp, Categories: "utility;"}); got != GroupApps {
		t.Errorf("GroupOf = %q, want apps", got)
	}
	if got := GroupOf(Entry{Kind: KindApp, Categories: ";"}); got != GroupApps {
		t.Errorf("an empty category list should fall back to apps, got %q", got)
	}
}

func TestGroupedBucketsAndOrders(t *testing.T) {
	entries := []Entry{
		{Name: "editor", Kind: KindApp, Categories: "Utility;"},
		{Name: "game", Kind: KindApp, Categories: "Game;"},
		{Name: "note.txt", Kind: KindFile, Path: "/x/note.txt"},
		{Name: "Photos", Kind: KindDir},
		{Name: "binary", Kind: KindFile, Path: "/x/binary"},
	}
	order := []string{GroupGames, GroupApps, GroupDocs, GroupFolders, GroupFiles}
	groups, flat := Grouped(entries, order)

	if len(flat) != len(entries) {
		t.Fatalf("the flat list has %d entries", len(flat))
	}
	want := []Group{
		{Key: GroupGames, Title: "Games", First: 0, Count: 1},
		{Key: GroupApps, Title: "Applications", First: 1, Count: 1},
		{Key: GroupDocs, Title: "Documents", First: 2, Count: 1},
		{Key: GroupFolders, Title: "Folders", First: 3, Count: 1},
		{Key: GroupFiles, Title: "Files", First: 4, Count: 1},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Errorf("groups = %+v\nwant %+v", groups, want)
	}
	// The flat list must follow the group order, otherwise the tiles would be
	// laid out over the wrong entries.
	if flat[0].Name != "game" || flat[1].Name != "editor" || flat[2].Name != "note.txt" {
		t.Errorf("flat order = %s, %s, %s", flat[0].Name, flat[1].Name, flat[2].Name)
	}
}

func TestGroupedUnknownGroupsFallBackToFiles(t *testing.T) {
	entries := []Entry{
		{Name: "media", Kind: KindFile, Path: "/x/a.mp3"},
		{Name: "text", Kind: KindFile, Path: "/x/a.txt"},
	}
	groups, flat := Grouped(entries, []string{GroupDocs})

	counts := map[string]int{}
	for _, g := range groups {
		counts[g.Key] = g.Count
	}
	// The document keeps its band; the song falls into the catch-all band
	// because "media" is not in the requested order.
	if counts[GroupDocs] != 1 || counts[GroupFiles] != 1 {
		t.Errorf("unlisted groups should land in Files, got %+v", groups)
	}
	if len(flat) != 2 {
		t.Errorf("nothing should be dropped, got %d entries", len(flat))
	}
	for i, g := range groups {
		if g.First != i {
			t.Errorf("group %d starts at %d", i, g.First)
		}
	}
}

func TestGroupedEmpty(t *testing.T) {
	groups, flat := Grouped(nil, nil)
	if len(groups) != 0 || len(flat) != 0 {
		t.Errorf("Grouped(nil) = %+v %+v", groups, flat)
	}
}

func TestTitleFor(t *testing.T) {
	if got := TitleFor(GroupApps); got != "Applications" {
		t.Errorf("TitleFor(apps) = %q", got)
	}
	if got := TitleFor("something-else"); got != "something-else" {
		t.Errorf("an unknown key should be its own title, got %q", got)
	}
}

func TestParseLauncherReadsCategories(t *testing.T) {
	entry, err := ParseLauncher("[Desktop Entry]\nName=Solitaire\nExec=aisleriot\nCategories=Game;CardGame;\n")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Categories != "Game;CardGame;" {
		t.Errorf("Categories = %q", entry.Categories)
	}
	if got := GroupOf(entry); got != GroupGames {
		t.Errorf("GroupOf = %q", got)
	}
}
