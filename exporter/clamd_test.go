package exporter

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/promslog"
)

func TestExporter_scrapeClamd(t *testing.T) {
	files, err := filepath.Glob("testdata/*-socket.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			in, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			exporter, err := New(nil, 0, 0, promslog.NewNopLogger())
			if err != nil {
				t.Fatalf("New() = _, %v; want nil", err)
			}
			exporter.scrape = func(e *Exporter) (m metrics, ok bool) {
				return e.scrapeClamd(bytes.Split(bytes.TrimSuffix(in, []byte("\n")), []byte("\n--\n")))
			}
			outFile := strings.Replace(file, "-socket.txt", "-metrics.txt", 1)
			if _, err := os.Stat(outFile); err == nil {
				out, err := os.ReadFile(outFile)
				if err != nil {
					t.Fatal(err)
				}
				if err = testutil.CollectAndCompare(exporter, bytes.NewReader(out)); err != nil {
					t.Errorf("testutil.CollectAndCompare() = %v; want nil", err)
				}
			} else {
				if err = os.WriteFile(outFile, collect(t, exporter), 0666); err != nil {
					t.Fatal(err)
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
	cmd := exec.Command("docker", "compose", "-f", "../testdata/docker/compose.yml", "exec", "clamav", "clamdscan", "-V")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "/")
	if len(parts) < 3 {
		t.Fatalf("failed to parse clamdscan -V output:\n%s", out)
	}
	ver, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	tm, err := time.Parse("Mon Jan 2 15:04:05 2006", parts[2])
	if err != nil {
		t.Fatal(err)
	}
	address, _ := url.Parse("tcp://127.0.0.1:3310")
	b, err := os.ReadFile("testdata/metrics-integration.txt")
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte("__CLAMAV_DB_VERSION__"), []byte(strconv.Itoa(ver)))
	b = bytes.ReplaceAll(b, []byte("__CLAMAV_DB_TIMESTAMP_SECONDS__"), []byte(strconv.FormatInt(tm.Unix(), 10)))
	metricNames := []string{
		"clamav_db_timestamp_seconds",
		"clamav_db_version",
		"clamav_pool_idle_timeout_threads",
		"clamav_pool_max_threads",
		"clamav_pool_state",
		"clamav_pool_queue_length",
		"clamav_up",
	}
	exporter, err := New(address, time.Second, 2, promslog.NewNopLogger())
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
		t.Fatal(err)
	}
	got, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.FmtText)
	for _, mf := range got {
		if err := enc.Encode(mf); err != nil {
			t.Fatal(err)
		}
	}
	if closer, ok := enc.(expfmt.Closer); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}
