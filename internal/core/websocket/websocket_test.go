package ws

import (
	"fmt"
	"sync"
	"testing"
)

func TestProgressCannotBlockWithoutConsumers(t *testing.T) {
	wm := New()
	for i := 0; i < 1000; i++ {
		wm.SendProgress(TorrentUpdate{ID: "one", Status: "downloading", Progress: float64(i)})
	}
	wm.mu.Lock()
	defer wm.mu.Unlock()
	if wm.activeDownloads["one"].Progress != 999 {
		t.Fatal("latest snapshot lost")
	}
}
func TestStartedStageIncludedInReconnectSnapshot(t *testing.T) {
	wm := New()
	wm.SendProgress(TorrentUpdate{ID: "one", Status: "pending"})
	wm.SendProgress(TorrentUpdate{ID: "one", Status: "started", Name: "metadata resolved"})
	wm.mu.Lock()
	defer wm.mu.Unlock()
	if wm.activeDownloads["one"].Status != "started" {
		t.Fatal("reconnect snapshot missed current metadata stage")
	}
}
func TestConcurrentSnapshotUpdates(t *testing.T) {
	wm := New()
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				wm.SendProgress(TorrentUpdate{ID: fmt.Sprint(n), Status: "downloading"})
			}
			wm.SendProgress(TorrentUpdate{ID: fmt.Sprint(n), Status: "completed"})
		}(n)
	}
	wg.Wait()
	wm.mu.Lock()
	defer wm.mu.Unlock()
	if len(wm.activeDownloads) != 0 {
		t.Fatal("terminal state retained")
	}
}
