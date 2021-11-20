# ClamAV Exporter

[![tests](https://github.com/sergeymakinen/clamav_exporter/workflows/tests/badge.svg)](https://github.com/sergeymakinen/clamav_exporter/actions?query=workflow%3Atests)
[![Go Reference](https://pkg.go.dev/badge/github.com/sergeymakinen/clamav_exporter.svg)](https://pkg.go.dev/github.com/sergeymakinen/clamav_exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/sergeymakinen/clamav_exporter)](https://goreportcard.com/report/github.com/sergeymakinen/clamav_exporter)
[![codecov](https://codecov.io/gh/sergeymakinen/clamav_exporter/branch/main/graph/badge.svg)](https://codecov.io/gh/sergeymakinen/clamav_exporter)

Export ClamAV daemon stats via a TCP socket to Prometheus.

To run it:

```bash
make
./clamav_exporter [flags]
```

## Exported metrics

| Metric | Meaning | Labels
| --- | --- | ---
| clamav_up | Was the last scrape successful. |
| clamav_db_version | Currently installed ClamAV Virus Database version. |
| clamav_db_timestamp_seconds | Unix timestamp of the ClamAV Virus Database build time. |
| clamav_pool_state | State of the thread pool. | index, primary
| clamav_pool_live_threads | Number of live threads in the pool. | index, primary
| clamav_pool_idle_threads | Number of idle threads in the pool. | index, primary
| clamav_pool_max_threads | Maximum number of threads in the pool. | index, primary
| clamav_pool_idle_timeout_threads | Number of idle timeout threads in the pool. | index, primary
| clamav_pool_queue_length | Number of items in the pool queue. | index, primary
| clamav_memory_heap_bytes | Number of bytes allocated on the heap. |
| clamav_memory_mmap_bytes | Number of bytes currently allocated using mmap. |
| clamav_memory_used_bytes | Number of bytes used by in-use allocations. |
| clamav_memory_free_bytes | Number of bytes in free blocks. |
| clamav_memory_releasable_bytes | Number of bytes releasable at the heap. |
| clamav_memory_pools_used_bytes | Number of bytes currently used by all pools. |
| clamav_memory_pools_total_bytes | Number of bytes available to all pools. |

### Pool state mapping

| Name | State value
| --- | ---
| INVALID | 0
| VALID | 1
| EXIT | 2

## Flags

```bash
./clamav_exporter --help
```

* __`clamav.address`:__ ClamAV daemon socket address. Example: `tcp://127.0.0.1:3310`.
* __`clamav.timeout`:__ ClamAV daemon socket timeout.
* __`clamav.retries`:__ ClamAV daemon socket connect retries. `0` by default.
* __`web.listen-address`:__ Address to listen on for web interface and telemetry.
* __`web.telemetry-path`:__ Path under which to expose metrics.
* __`log.level`:__ Logging level. `info` by default.
* __`log.format`:__ Set the log target and format. Example: `logger:syslog?appname=bob&local=7`
  or `logger:stdout?json=true`.

### TLS and basic authentication

The clamav_exporter supports TLS and basic authentication.
To use TLS and/or basic authentication, you need to pass a configuration file
using the `--web.config.file` parameter. The format of the file is described
[in the exporter-toolkit repository](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md).
