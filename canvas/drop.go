package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// debugDND turns on the tracing of the drag and drop operations.
var debugDND = os.Getenv("CANVAS_DEBUG_DND") == "1"

// folderAt reports the folder the pointer is over while icons are being
// dragged. The folders that are dragged themselves are ignored: a folder
// cannot be dropped into itself.
func (v *view) folderAt(sx, sy int) int {
	bx := v.ox + float64(sx)/v.zoom
	by := v.oy + float64(sy)/v.zoom
	for i, ic := range v.icons {
		if !ic.isDir || ic.up {
			continue
		}
		if v.selset[i] {
			continue
		}
		if bx >= ic.x && bx < ic.x+ic.w && by >= ic.y && by < ic.y+ic.h {
			return i
		}
	}
	return -1
}

// dropIntoFolder moves the dragged icons into the folder they were dropped
// on and refreshes the board.
func (v *view) dropIntoFolder(target int) {
	if target < 0 || target >= len(v.icons) {
		return
	}
	dst := v.icons[target]
	if !dst.isDir || dst.up {
		return
	}
	var moving []int
	for i := range v.selset {
		if i >= 0 && i < len(v.icons) {
			moving = append(moving, i)
		}
	}
	sort.Ints(moving)
	moved, failed := 0, 0
	for _, i := range moving {
		ic := v.icons[i]
		if ic.path == dst.path {
			continue
		}
		if err := moveIntoFolder(ic.path, dst.path); err != nil {
			failed++
			if debugDND {
				fmt.Printf("dnd: move %s -> %s: %v\n", ic.path, dst.path, err)
			}
			v.setStatus("Ошибка: " + err.Error())
			continue
		}
		moved++
	}
	if moved == 0 {
		return
	}
	if failed == 0 {
		v.setStatus(fmt.Sprintf("В папку %s: %d", dst.name, moved))
	} else {
		v.setStatus(fmt.Sprintf("В папку %s: %d, с ошибкой: %d", dst.name, moved, failed))
	}
	v.reload(false)
	savePositions(v.root, v.icons)
}

// freeSpot looks for a free cell near a board point, so that the icons of
// the dropped files do not cover the ones that are already there.
func (v *view) freeSpot(bx, by float64) (float64, float64) {
	x := bx - cellW/2
	y := by - tileTop - cellH/2
	for attempt := 0; attempt < 60; attempt++ {
		clash := false
		for _, ic := range v.icons {
			if x < ic.x+ic.w && ic.x < x+cellW && y < ic.y+ic.h && ic.y < y+cellH {
				clash = true
				break
			}
		}
		if !clash {
			return x, y
		}
		y += cellH + gapY
	}
	return x, y
}

// moveIntoFolder moves a file or a folder into a folder. A name clash is
// resolved with a numbered copy, and a move across file systems falls back to
// a copy followed by a delete.
func moveIntoFolder(src, dstDir string) error {
	base := filepath.Base(src)
	dst := filepath.Join(dstDir, base)
	if src == dst {
		return nil
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		// A folder cannot be moved into itself or into one of its children.
		if dst == src || strings.HasPrefix(dst, src+string(filepath.Separator)) {
			return errors.New("папку нельзя переместить в саму себя")
		}
	}
	if exists(dst) {
		dst = uniquePath(dst)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}
