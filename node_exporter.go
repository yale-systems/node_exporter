// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/node_exporter/collector"
	"github.com/yale-systems/osdb/pkg/dbsc"
)

func main() {
	var (
		disableDefaultCollectors = kingpin.Flag(
			"collector.disable-defaults",
			"Set all collectors to disabled by default.",
		).Default("false").Bool()
		maxProcs = kingpin.Flag(
			"runtime.gomaxprocs", "The target number of CPUs Go will run on (GOMAXPROCS)",
		).Envar("GOMAXPROCS").Default("1").Int()
	)

	kingpin.Parse()

	logger := slog.Default()
	if *disableDefaultCollectors {
		collector.DisableDefaultCollectors()
	}
	if user, err := user.Current(); err == nil && user.Uid == "0" {
		logger.Warn("Node Exporter is running as root user. This exporter is designed to run as unprivileged user, root is not required.")
	}
	runtime.GOMAXPROCS(*maxProcs)
	logger.Debug("Go MAXPROCS", "procs", runtime.GOMAXPROCS(0))

	nodeCollector, err := collector.NewNodeCollector(logger)
	if err != nil {
		logger.Error("could no create collector", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registry := prometheus.NewRegistry()
	registry.MustRegister(versioncollector.NewCollector("node_exporter"))
	if err := registry.Register(nodeCollector); err != nil {
		logger.Error("could not register node collector", "error", err)
		os.Exit(1)
	}

	client, err := dbsc.NewClient("node_exporter", dbsc.WithPrometheusDataSource(registry))
	if err != nil {
		slog.Error("failed to initialize dbsc client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	ch := make(chan error, 1)
	go func() {
		ch <- client.Run()
	}()

	select {
	case err := <-ch:
		if err != nil {
			slog.Error("client failure", "error", err)
			os.Exit(1)
		}

	case <-ctx.Done():
	}

}
