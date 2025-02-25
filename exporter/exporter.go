// Package exporter provides a collector for ClamAV daemon stats.
package exporter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "clamav"

var (
	reVersion    = regexp.MustCompile(`ClamAV (.+)/(\d+)/(.+)`)
	rePool       = regexp.MustCompile(`STATE: ([^\n]+)\nTHREADS: ([^\n]+)\nQUEUE: ([^\n]+)\n`)
	reThreadStat = regexp.MustCompile(`([a-z\-]+) (\d+)`)
	reQueue      = regexp.MustCompile(`(\d+) items min_wait: (\d+\.\d+) max_wait: (\d+\.\d+) avg_wait: (\d+\.\d+)`)
	reMemStats   = regexp.MustCompile(`MEMSTATS: (.+)`)
	reMemStat    = regexp.MustCompile(`([a-z_]+) ([\d.]+)M`)
)

var states = map[string]float64{
	"INVALID": 0,
	"VALID":   1,
	"EXIT":    2,
}

var tz = time.Local

// Exporter collects ClamAV daemon stats via a TCP socket and exports them
// using the prometheus metrics package.
type Exporter struct {
	scrape  func(e *Exporter) (m metrics, ok bool)
	address *url.URL
	timeout time.Duration
	retries int
	logger  *slog.Logger
	mu      sync.Mutex

	up                     *prometheus.Desc
	version                *prometheus.Desc
	dbVersion              *prometheus.Desc
	dbTime                 *prometheus.Desc
	poolState              *prometheus.Desc
	poolLiveThreads        *prometheus.Desc
	poolIdleThreads        *prometheus.Desc
	poolMaxThreads         *prometheus.Desc
	poolIdleTimeoutThreads *prometheus.Desc
	poolQueueLength        *prometheus.Desc
	poolQueueMinWait       *prometheus.Desc
	poolQueueMaxWait       *prometheus.Desc
	poolQueueAvgWait       *prometheus.Desc
	heapMemory             *prometheus.Desc
	mmapMemory             *prometheus.Desc
	usedMemory             *prometheus.Desc
	freeMemory             *prometheus.Desc
	releasableMemory       *prometheus.Desc
	poolsUsedMemory        *prometheus.Desc
	poolsTotalMemory       *prometheus.Desc
}

// Describe describes all the metrics exported by the ClamAV exporter. It
// implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up
	ch <- e.version
	ch <- e.dbVersion
	ch <- e.dbTime
	ch <- e.poolState
	ch <- e.poolLiveThreads
	ch <- e.poolIdleThreads
	ch <- e.poolMaxThreads
	ch <- e.poolIdleTimeoutThreads
	ch <- e.poolQueueLength
	ch <- e.poolQueueMinWait
	ch <- e.poolQueueMaxWait
	ch <- e.poolQueueAvgWait
	ch <- e.heapMemory
	ch <- e.mmapMemory
	ch <- e.usedMemory
	ch <- e.freeMemory
	ch <- e.releasableMemory
	ch <- e.poolsUsedMemory
	ch <- e.poolsTotalMemory
}

// Collect fetches the statistics from ClamAV, and
// delivers them as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	m, ok := e.scrape(e)
	if !ok {
		ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, 0)
		return
	}

	e.collect(m, ch)
}

func (e *Exporter) scrapeSocket() (m metrics, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var resp [][]byte
	scrape := func(retries int) bool {
		network, addr := e.address.Scheme, e.address.Host
		if network == "unix" {
			addr = e.address.Path
		}
		conn, err := net.DialTimeout(network, addr, e.timeout)
		if err != nil {
			e.logger.Error("Failed to connect to clamd", "err", err, "retries", retries)
			return false
		}
		defer conn.Close()
		// Following the recommendations:
		// 	Clamd requires clients to read all the replies it sent, before sending more commands to prevent send()
		// 	deadlocks. The recommended way to implement a client that uses IDSESSION is with non-blocking sockets,
		// 	and a select()/poll() loop: whenever send would block, sleep in select/poll until either you can write
		// 	more data, or read more replies.
		var mu sync.Mutex
		send := func(cmd string) bool {
			mu.Lock()
			defer mu.Unlock()
			conn.SetWriteDeadline(time.Now().Add(e.timeout))
			if _, err := conn.Write([]byte("z" + cmd + "\000")); err != nil {
				e.logger.Error("Failed to send command", "cmd", cmd, "err", err, "retries", retries)
				return false
			}
			return true
		}
		resp = make([][]byte, 3)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				conn.SetReadDeadline(time.Now().Add(e.timeout))
				if _, err = conn.Read(nil); err != nil {
					e.logger.Error("Failed to read response", "err", err, "retries", retries)
					break
				}
				mu.Lock()
				var b []byte
				b, err = io.ReadAll(conn)
				mu.Unlock()
				if err != nil {
					e.logger.Error("Failed to read response", "err", err, "retries", retries)
					break
				}
				if err = parseResponse(b, resp); err != nil {
					e.logger.Error("Failed to parse response", "err", err, "retries", retries)
					break
				}
				if len(b) == 0 {
					break
				}
			}
		}()
		send("IDSESSION")
		send("PING")
		send("VERSION")
		send("STATS")
		send("END")
		wg.Wait()
		if err != nil {
			return false
		}
		return true
	}
	for retries := e.retries; retries >= 0; retries-- {
		if scrape(retries) {
			return e.scrapeClamd(resp)
		}
	}
	return
}

func (e *Exporter) scrapeClamd(resp [][]byte) (m metrics, ok bool) {
	if !bytes.Equal(resp[0], []byte("PONG")) {
		e.logger.Error("Unexpected PING response", "resp", resp[0])
		return
	}
	matches := reVersion.FindStringSubmatch(string(resp[1]))
	if matches != nil {
		m.Version = &matches[1]
		n, _ := strconv.ParseUint(matches[2], 10, 32)
		m.DB = &db{
			Version: uint32(n),
			Time:    matches[3],
		}
	}
	for _, poolMatches := range rePool.FindAllStringSubmatch(string(resp[2]), -1) {
		var pool pool
		for _, s := range strings.Split(poolMatches[1], " ") {
			if _, ok := states[s]; ok {
				pool.State = s
			} else if s == "PRIMARY" {
				pool.Primary = true
			}
		}
		for _, statMatches := range reThreadStat.FindAllStringSubmatch(poolMatches[2], -1) {
			n, _ := strconv.ParseInt(statMatches[2], 10, 64)
			switch statMatches[1] {
			case "live":
				pool.Threads.Live = &n
			case "idle":
				pool.Threads.Idle = &n
			case "max":
				pool.Threads.Max = &n
			case "idle-timeout":
				pool.Threads.IdleTimeout = &n
			}
		}
		matches = reQueue.FindStringSubmatch(poolMatches[3])
		if matches != nil {
			pool.Queue.Length, _ = strconv.ParseInt(matches[1], 10, 64)
			pool.Queue.MinWait, _ = strconv.ParseFloat(matches[2], 64)
			pool.Queue.MaxWait, _ = strconv.ParseFloat(matches[3], 64)
			pool.Queue.AvgWait, _ = strconv.ParseFloat(matches[4], 64)
		}
		m.Pools = append(m.Pools, pool)
	}
	matches = reMemStats.FindStringSubmatch(string(resp[2]))
	if matches != nil {
		for _, statMatches := range reMemStat.FindAllStringSubmatch(matches[1], -1) {
			f, _ := strconv.ParseFloat(statMatches[2], 64)
			n := uint64(f * 1024 * 1024)
			switch statMatches[1] {
			case "heap":
				m.Memory.Heap = &n
			case "mmap":
				m.Memory.Mmap = &n
			case "used":
				m.Memory.Used = &n
			case "free":
				m.Memory.Free = &n
			case "releasable":
				m.Memory.Releasable = &n
			case "pools_used":
				m.Memory.PoolsUsed = &n
			case "pools_total":
				m.Memory.PoolsTotal = &n
			}
		}
	}
	ok = true
	return
}

func (e *Exporter) collect(m metrics, ch chan<- prometheus.Metric) {
	if m.Version != nil {
		ch <- prometheus.MustNewConstMetric(e.version, prometheus.GaugeValue, float64(1), *m.Version)
	}
	if m.DB != nil {
		ch <- prometheus.MustNewConstMetric(e.dbVersion, prometheus.GaugeValue, float64(m.DB.Version))
		t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", m.DB.Time, tz)
		if err != nil {
			ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, 1)
			e.logger.Error("Failed to parse database time", "time", m.DB.Time, "err", err)
			return
		}
		ch <- prometheus.MustNewConstMetric(e.dbTime, prometheus.GaugeValue, float64(t.Unix()))
	}
	for i, pool := range m.Pools {
		primary := "0"
		if pool.Primary {
			primary = "1"
		}
		labelValues := []string{
			strconv.FormatInt(int64(i), 10),
			primary,
		}
		if pool.State != "" {
			ch <- prometheus.MustNewConstMetric(e.poolState, prometheus.GaugeValue, states[pool.State], labelValues...)
		}
		if pool.Threads.Live != nil {
			ch <- prometheus.MustNewConstMetric(e.poolLiveThreads, prometheus.GaugeValue, float64(*pool.Threads.Live), labelValues...)
		}
		if pool.Threads.Idle != nil {
			ch <- prometheus.MustNewConstMetric(e.poolIdleThreads, prometheus.GaugeValue, float64(*pool.Threads.Idle), labelValues...)
		}
		if pool.Threads.Max != nil {
			ch <- prometheus.MustNewConstMetric(e.poolMaxThreads, prometheus.GaugeValue, float64(*pool.Threads.Max), labelValues...)
		}
		if pool.Threads.IdleTimeout != nil {
			ch <- prometheus.MustNewConstMetric(e.poolIdleTimeoutThreads, prometheus.GaugeValue, float64(*pool.Threads.IdleTimeout), labelValues...)
		}
		ch <- prometheus.MustNewConstMetric(e.poolQueueLength, prometheus.GaugeValue, float64(pool.Queue.Length), labelValues...)
		ch <- prometheus.MustNewConstMetric(e.poolQueueMinWait, prometheus.GaugeValue, pool.Queue.MinWait, labelValues...)
		ch <- prometheus.MustNewConstMetric(e.poolQueueMaxWait, prometheus.GaugeValue, pool.Queue.MaxWait, labelValues...)
		ch <- prometheus.MustNewConstMetric(e.poolQueueAvgWait, prometheus.GaugeValue, pool.Queue.AvgWait, labelValues...)
	}
	if m.Memory.Heap != nil {
		ch <- prometheus.MustNewConstMetric(e.heapMemory, prometheus.GaugeValue, float64(*m.Memory.Heap))
	}
	if m.Memory.Mmap != nil {
		ch <- prometheus.MustNewConstMetric(e.mmapMemory, prometheus.GaugeValue, float64(*m.Memory.Mmap))
	}
	if m.Memory.Used != nil {
		ch <- prometheus.MustNewConstMetric(e.usedMemory, prometheus.GaugeValue, float64(*m.Memory.Used))
	}
	if m.Memory.Free != nil {
		ch <- prometheus.MustNewConstMetric(e.freeMemory, prometheus.GaugeValue, float64(*m.Memory.Free))
	}
	if m.Memory.Releasable != nil {
		ch <- prometheus.MustNewConstMetric(e.releasableMemory, prometheus.GaugeValue, float64(*m.Memory.Releasable))
	}
	if m.Memory.PoolsUsed != nil {
		ch <- prometheus.MustNewConstMetric(e.poolsUsedMemory, prometheus.GaugeValue, float64(*m.Memory.PoolsUsed))
	}
	if m.Memory.PoolsTotal != nil {
		ch <- prometheus.MustNewConstMetric(e.poolsTotalMemory, prometheus.GaugeValue, float64(*m.Memory.PoolsTotal))
	}
	ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, 1)
}

func parseResponse(data []byte, resp [][]byte) error {
	for {
		if len(data) == 0 {
			return nil
		}
		i := bytes.Index(data, []byte(": "))
		if i == -1 {
			return errors.New("failed to find response ID")
		}
		n, err := strconv.ParseInt(string(data[:i]), 10, 64)
		if err != nil {
			return errors.New("invalid response ID: " + err.Error())
		}
		if n < 1 || n-1 >= int64(len(resp)) {
			return errors.New("response ID out of range")
		}
		data = data[i+2:]
		i = bytes.IndexByte(data, '\000')
		if i == -1 {
			return errors.New("missing trailing NULL")
		}
		resp[n-1] = data[:i]
		data = data[i+1:]
	}
}

// New returns an initialized exporter.
func New(address *url.URL, timeout time.Duration, retries int, logger *slog.Logger) (*Exporter, error) {
	if retries < 0 {
		return nil, fmt.Errorf("invalid retry count %d", retries)
	}
	return &Exporter{
		scrape:  (*Exporter).scrapeSocket,
		address: address,
		timeout: timeout,
		retries: retries,
		logger:  logger,

		up: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "up"),
			"Was the last scrape successful.",
			nil,
			nil,
		),
		version: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "version"),
			"The version of this ClamAV.",
			[]string{"version"},
			nil,
		),
		dbVersion: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "db_version"),
			"Currently installed ClamAV Virus Database version.",
			nil,
			nil,
		),
		dbTime: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "db_timestamp_seconds"),
			"Unix timestamp of the ClamAV Virus Database build time.",
			nil,
			nil,
		),
		poolState: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_state"),
			"State of the thread pool.",
			[]string{"index", "primary"},
			nil,
		),
		poolLiveThreads: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_live_threads"),
			"Number of live threads in the pool.",
			[]string{"index", "primary"},
			nil,
		),
		poolIdleThreads: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_idle_threads"),
			"Number of idle threads in the pool.",
			[]string{"index", "primary"},
			nil,
		),
		poolMaxThreads: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_max_threads"),
			"Maximum number of threads in the pool.",
			[]string{"index", "primary"},
			nil,
		),
		poolIdleTimeoutThreads: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_idle_timeout_threads"),
			"Number of idle timeout threads in the pool.",
			[]string{"index", "primary"},
			nil,
		),
		poolQueueLength: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_queue_length"),
			"Number of items in the pool queue.",
			[]string{"index", "primary"},
			nil,
		),
		poolQueueMinWait: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_queue_min_wait_sec"),
			"Minimum wait time in the pool queue.",
			[]string{"index", "primary"},
			nil,
		),
		poolQueueMaxWait: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_queue_max_wait_sec"),
			"Maximum wait time in the pool queue.",
			[]string{"index", "primary"},
			nil,
		),
		poolQueueAvgWait: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "pool_queue_avg_wait_sec"),
			"Average wait time in the pool queue.",
			[]string{"index", "primary"},
			nil,
		),
		heapMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_heap_bytes"),
			"Number of bytes allocated on the heap.",
			nil,
			nil,
		),
		mmapMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_mmap_bytes"),
			"Number of bytes currently allocated using mmap.",
			nil,
			nil,
		),
		usedMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_used_bytes"),
			"Number of bytes used by in-use allocations.",
			nil,
			nil,
		),
		freeMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_free_bytes"),
			"Number of bytes in free blocks.",
			nil,
			nil,
		),
		releasableMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_releasable_bytes"),
			"Number of bytes releasable at the heap.",
			nil,
			nil,
		),
		poolsUsedMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_pools_used_bytes"),
			"Number of bytes currently used by all pools.",
			nil,
			nil,
		),
		poolsTotalMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_pools_total_bytes"),
			"Number of bytes available to all pools.",
			nil,
			nil,
		),
	}, nil
}
