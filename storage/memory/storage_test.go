package memory

import (
	"testing"
	"time"

	"github.com/sot-tech/mochi/storage"
	"github.com/sot-tech/mochi/storage/test"
)

func createNew() storage.PeerStorage {
	ps, err := NewPeerStorage(Config{
		ShardCount:                  1024,
		GarbageCollectionInterval:   10 * time.Minute,
		PrometheusReportingInterval: 10 * time.Minute,
		PeerLifetime:                30 * time.Minute,
	})
	if err != nil {
		panic(err)
	}
	return ps
}

func TestStorage(t *testing.T) { test.RunTests(t, createNew()) }

func BenchmarkStorage(b *testing.B) { test.RunBenchmarks(b, createNew) }
