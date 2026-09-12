package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"req/internal/model"
	"sync"
	"testing"
)

func TestCollectionTrailingDelimiter(t *testing.T) {
	w := newTestWorkspace(t)
	c, _ := saveFixture(t, w)
	p := filepath.Join(w.Dir(), "collections", c.ID+".json")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(b, ']'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.LoadCollection(context.Background(), c.ID); err == nil {
		t.Fatal("trailing delimiter accepted")
	}
}

func TestConcurrentCollectionNames(t *testing.T) {
	w := newTestWorkspace(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- w.SaveCollection(context.Background(), model.Collection{SchemaVersion: 1, ID: model.NewID("col"), Name: "same"}, "")
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrDuplicateName) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d winners", winners)
	}
}
