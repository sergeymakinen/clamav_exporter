package exporter

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMain(m *testing.M) {
	tz = time.UTC
	os.Exit(m.Run())
}

func TestExporter_Collect(t *testing.T) {
	exporter, err := New(nil, 0, 0, log.NewNopLogger())
	if err != nil {
		t.Fatalf("New() = _, %v; want nil", err)
	}
	exporter.scrape = func(e *Exporter) (m metrics, ok bool) {
		return metrics{
			DB: &db{
				Version: 123,
				Time:    "Fri Nov 19 09:19:46 2021",
			},
			Pools: []pool{
				{
					State:   "EXIT",
					Primary: true,
					Threads: threads{
						Live:        newInt64(124),
						Max:         newInt64(125),
						IdleTimeout: newInt64(126),
					},
					Queue: queue{
						Length:  127,
						MinWait: 0.131,
						MaxWait: 0.132,
						AvgWait: 0.133,
					},
				},
			},
			Memory: memory{
				Heap:       newUint64(128),
				Mmap:       newUint64(0),
				PoolsUsed:  newUint64(129 * 1024),
				PoolsTotal: newUint64(130 * 1024 * 1024),
			},
		}, true
	}
	b, err := ioutil.ReadFile("testdata/metrics.txt")
	if err != nil {
		panic("failed to read testdata/metrics.txt: " + err.Error())
	}
	if err := testutil.CollectAndCompare(exporter, bytes.NewReader(b)); err != nil {
		t.Errorf("testutil.CollectAndCompare() = %v; want nil", err)
	}
}

func newInt64(n int64) *int64    { return &n }
func newUint64(n uint64) *uint64 { return &n }
