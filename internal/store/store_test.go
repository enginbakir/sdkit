package store

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestUpdateCounter(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Update(p, func(old []byte) ([]byte, error) {
				n, _ := strconv.Atoi(string(old))
				return []byte(strconv.Itoa(n + 1)), nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if b, _ := os.ReadFile(p); string(b) != "20" {
		t.Fatalf("counter = %q", b)
	}
}

func TestStaleLockTakenOver(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.Mkdir(p+".lock", 0o755)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(p+".lock", old, old)
	unlock, err := Lock(p)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
