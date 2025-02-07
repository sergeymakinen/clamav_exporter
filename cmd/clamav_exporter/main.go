package main

import (
	"net/http"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"github.com/sergeymakinen/clamav_exporter/v2/exporter"
)

func main() {
	var (
		address      = kingpin.Flag("clamav.address", "ClamAV daemon socket address.").PlaceHolder(`"tcp://127.0.0.1:3310"`).Default("tcp://127.0.0.1:3310").URL()
		timeout      = kingpin.Flag("clamav.timeout", "ClamAV daemon socket timeout.").Default("5s").Duration()
		retries      = kingpin.Flag("clamav.retries", "ClamAV daemon socket connect retries.").Default("0").Int()
		toolkitFlags = webflag.AddFlags(kingpin.CommandLine, ":9906")
		metricsPath  = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	)
	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print("clamav_exporter"))
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promslog.New(promslogConfig)

	logger.Info("Starting clamav_exporter", "version", version.Info())
	logger.Info("Build context", "context", version.BuildContext())

	prometheus.MustRegister(versioncollector.NewCollector("clamav_exporter"))
	exporter, err := exporter.New(*address, *timeout, *retries, logger)
	if err != nil {
		logger.Error("Error creating the exporter", "err", err)
		os.Exit(1)
	}
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, promhttp.Handler())
	if *metricsPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "ClamAV Exporter",
			Description: "Prometheus Exporter for ClamAV",
			Version:     version.Info(),
			Links: []web.LandingLinks{
				{
					Address: *metricsPath,
					Text:    "Metrics",
				},
			},
		}
		landingPage, err := web.NewLandingPage(landingConfig)
		if err != nil {
			logger.Error(err.Error())
			os.Exit(1)
		}
		http.Handle("/", landingPage)
	}

	srv := &http.Server{}
	if err := web.ListenAndServe(srv, toolkitFlags, logger); err != nil {
		logger.Error("Error running HTTP server", "err", err)
		os.Exit(1)
	}
}
