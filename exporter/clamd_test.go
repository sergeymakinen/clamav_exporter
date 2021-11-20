package exporter

import (
	"bytes"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/expfmt"
)

func TestExporter_scrapeClamd(t *testing.T) {
	files, err := filepath.Glob("testdata/*-socket.txt")
	if err != nil {
		panic("failed to list test files: " + err.Error())
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			in, err := ioutil.ReadFile(file)
			if err != nil {
				panic("failed to read " + file + ": " + err.Error())
			}
			exporter, err := New(nil, 0, 0, log.NewNopLogger())
			if err != nil {
				t.Fatalf("New() = _, %v; want nil", err)
			}
			exporter.scrape = func(e *Exporter) (m metrics, ok bool) {
				return e.scrapeClamd(bytes.Split(bytes.TrimSuffix(in, []byte("\n")), []byte("\n--\n")))
			}
			outFile := strings.Replace(file, "-socket.txt", "-metrics.txt", 1)
			if _, err := os.Stat(outFile); err == nil {
				out, err := ioutil.ReadFile(outFile)
				if err != nil {
					panic("failed to read " + outFile + ": " + err.Error())
				}
				if err = testutil.CollectAndCompare(exporter, bytes.NewReader(out)); err != nil {
					t.Errorf("testutil.CollectAndCompare() = %v; want nil", err)
				}
			} else {
				if err = ioutil.WriteFile(outFile, collect(t, exporter), 0666); err != nil {
					panic("failed to write " + outFile + ": " + err.Error())
				}
				t.Logf("wrote %s golden master", outFile)
			}
		})
	}
}

func TestExporter_Collect_Clamd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestExporter_Collect_Clamd during short test")
	}
	address, _ := url.Parse("tcp://127.0.0.1:3310")
	b, err := ioutil.ReadFile("testdata/metrics-integration.txt")
	if err != nil {
		panic("failed to read testdata/metrics-integration.txt: " + err.Error())
	}
	metricNames := []string{
		"clamav_db_timestamp_seconds",
		"clamav_db_version",
		"clamav_pool_idle_timeout_threads",
		"clamav_pool_max_threads",
		"clamav_pool_state",
		"clamav_pool_queue_length",
		"clamav_up",
	}
	exporter, err := New(address, time.Second, 2, log.NewNopLogger())
	if err != nil {
		t.Fatalf("New() = _, %v; want nil", err)
	}
	if err = testutil.CollectAndCompare(exporter, bytes.NewReader(b), metricNames...); err != nil {
		t.Errorf("testutil.CollectAndCompare() = %v; want nil", err)
	}
}

func collect(t *testing.T, c prometheus.Collector) []byte {
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("failed to register exporter: %v", err)
	}
	got, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.FmtText)
	for _, mf := range got {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("failed to encode metric: %v", err)
		}
	}
	return buf.Bytes()
}
