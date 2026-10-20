package appconfig

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentFirstConfigWritersUseOneCompleteHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.key")
	const writers = 32
	keys := make([][]byte, writers)
	errs := make([]error, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			keys[i], errs[i] = LoadOrCreateHostKey(path)
		})
	}
	close(start)
	wg.Wait()
	stored, err := LoadHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range writers {
		if errs[i] != nil {
			t.Fatalf("writer %d: %v", i, errs[i])
		}
		box, err := Encrypt(ConfigRef{AppID: "app", Name: "SECRET"}, keys[i], []byte("fixture-secret"))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := Decrypt(ConfigRef{AppID: "app", Name: "SECRET"}, stored, box)
		if err != nil || !bytes.Equal(plain, []byte("fixture-secret")) {
			t.Fatalf("writer %d encrypted with a replaced key: %v", i, err)
		}
	}
}
