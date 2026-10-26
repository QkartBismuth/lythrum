package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// 5.4 — битый JSON: Load возвращает чистую ошибку (не паникует), а Save
// поверх битого файла восстанавливает читаемость.
func TestSessionCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	m := NewManager(path)

	if err := os.WriteFile(path, []byte("{это не json((( "), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Load(); err == nil {
		t.Fatal("битый JSON должен давать ошибку Load")
	}

	// Сохранение поверх битого — файл снова читаем.
	if err := m.Save(Snapshot{Camera: CameraSnapshot{X: 1, Zoom: 2}}); err != nil {
		t.Fatalf("Save поверх битого: %v", err)
	}
	snap, err := m.Load()
	if err != nil {
		t.Fatalf("Load после восстановления: %v", err)
	}
	if snap.Camera.Zoom != 2 {
		t.Fatalf("снапшот исказился: %+v", snap)
	}
}

// 5.7 — атомарность: осиротевший .tmp (крах между write и rename) не
// влияет на чтение основного файла.
func TestSessionOrphanTmpIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	m := NewManager(path)

	if err := m.Save(Snapshot{Camera: CameraSnapshot{X: 7, Zoom: 3}}); err != nil {
		t.Fatal(err)
	}
	// «Kill -9 в середине следующего автосейва»: мусор в .tmp.
	if err := os.WriteFile(path+".tmp", []byte("недописанный мусор"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := m.Load()
	if err != nil {
		t.Fatalf("Load с мусорным .tmp: %v", err)
	}
	if snap.Camera.X != 7 {
		t.Fatalf("снапшот исказился: %+v", snap)
	}

	// Следующий Save затирает осиротевший .tmp штатно.
	if err := m.Save(Snapshot{Camera: CameraSnapshot{X: 8}}); err != nil {
		t.Fatalf("Save с мусорным .tmp: %v", err)
	}
	if snap, _ := m.Load(); snap.Camera.X != 8 {
		t.Fatalf("после перезаписи: %+v", snap)
	}
}

// 5.7 — конкурентные автосейвы и читатели: каждый успешный Load валиден.
// При tmp+rename читатель видит ЛИБО старый файл, ЛИБО новый — никогда
// половинчатый.
func TestSessionConcurrentSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	m := NewManager(path)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for range 4 { // писатели (автосейвы)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if err := m.Save(Snapshot{Camera: CameraSnapshot{X: float64(i)}}); err != nil {
					t.Errorf("Save: %v", err)
					return
				}
			}
		}()
	}
	for range 4 { // читатели: любой увиденный файл обязан парситься
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := m.Load(); err != nil && !os.IsNotExist(err) {
					t.Errorf("Load вернул невалидный файл: %v", err)
					return
				}
			}
		}()
	}

	// Рабочий цикл автосейва.
	for range 300 {
		m.Save(Snapshot{Camera: CameraSnapshot{X: 42}})
	}
	close(stop)
	wg.Wait()
}
