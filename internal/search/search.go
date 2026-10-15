// Package search matches what the user types against applications and files.
// The matcher is deliberately small: a subsequence match with a score, so
// "gimp" finds "GNU Image Manipulation Program" and "rhythmbox" quickly.
package search

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Item is something the search can offer.
type Item struct {
	// Name is what the user reads.
	Name string
	// Path is the file or the launcher behind the name.
	Path string
	// Kind groups results: applications first, then files.
	Kind string
	// KindRank orders groups without comparing strings.
	KindRank int
}

// Result is a scored match.
type Result struct {
	Item
	Score int
	// Matched tells where the highlights go.
	Matched []int
}

// Match scores a query against a name. It returns false when the query is not
// a subsequence of the name, which is what filters the list. A query that
// appears as one piece scores far above a scattered match, so typing "clip"
// puts clip.mp4 on top instead of every name that happens to contain those
// letters.
func Match(query, name string) (int, bool) {
	if query == "" {
		return 0, true
	}
	lowerQuery := strings.ToLower(query)
	lowerName := strings.ToLower(name)

	score := 0
	if at := strings.Index(lowerName, lowerQuery); at >= 0 {
		score = 40 + len(lowerName)/8
		if at == 0 || isBoundary(lowerName, at) {
			score += 20
		}
	}

	next := 0
	lastIndex := -1
	matched := make([]int, 0, len(lowerQuery))
	for i := 0; i < len(lowerName) && next < len(lowerQuery); {
		if rune(lowerName[i]) != rune(lowerQuery[next]) {
			i++
			continue
		}
		gap := 0
		if lastIndex >= 0 {
			gap = i - lastIndex - 1
			// Long jumps cost more than short ones, and the cost is capped so
			// "gimp" still finds "GNU Image Manipulation Program".
			penalty := gap * 3
			if penalty > maxGapPenalty {
				penalty = maxGapPenalty
			}
			score -= penalty
		}
		switch {
		case i == 0:
			score += 12
		case isBoundary(lowerName, i):
			score += 10
		case gap == 0:
			score += 6
		}
		matched = append(matched, i)
		lastIndex = i
		next++
		i++
	}
	if next < len(lowerQuery) {
		return 0, false
	}
	// Shorter names that match are usually the better answer.
	score += len(lowerName) / 8
	return score, true
}

// maxGapPenalty caps what a scattered match loses, which keeps an acronym like
// "gimp" findable while still ranking it below a real substring.
const maxGapPenalty = 24

// isBoundary reports whether the position starts a word.
func isBoundary(name string, i int) bool {
	if i == 0 {
		return true
	}
	prev := rune(name[i-1])
	cur := rune(name[i])
	return unicode.IsSpace(prev) || prev == '-' || prev == '_' || prev == '.' ||
		(unicode.IsLower(prev) && unicode.IsUpper(cur))
}

// Rank sorts the items for a query, keeping the original order between equally
// good matches.
func Rank(query string, items []Item, limit int) []Result {
	trimmed := strings.TrimSpace(query)
	var results []Result
	for _, item := range items {
		score, ok := Match(trimmed, item.Name)
		if !ok {
			continue
		}
		// Match the file name too, so typing part of "report.pdf" works.
		if !ok {
			base := filepath.Base(item.Name)
			if s, hit := Match(trimmed, base); hit {
				score = s / 2
				ok = true
			}
		}
		if !ok {
			continue
		}
		results = append(results, Result{Item: item, Score: score + item.KindRank})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Name < results[j].Name
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

// Highlighted returns the name split into matched and unmatched runs, which
// the shell draws to show why a result matched.
func Highlighted(name string, matched []int) []Run {
	if len(matched) == 0 {
		return []Run{{Text: name}}
	}
	set := make(map[int]bool, len(matched))
	for _, i := range matched {
		set[i] = true
	}
	var runs []Run
	start := 0
	hit := set[0]
	for i := 1; i <= len(name); i++ {
		if i < len(name) && set[i] == hit {
			continue
		}
		runs = append(runs, Run{Text: name[start:i], Hit: hit})
		start = i
		if i < len(name) {
			hit = set[i]
		}
	}
	return runs
}

// Run is a piece of a name, either matched or not.
type Run struct {
	Text string
	Hit  bool
}
