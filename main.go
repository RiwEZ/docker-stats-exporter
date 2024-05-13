package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var daemonOsType string

func calcCPUPercentUnix(v *types.StatsJSON) float64 {
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(v.CPUStats.CPUUsage.TotalUsage) - float64(v.PreCPUStats.CPUUsage.TotalUsage)
		// calculate the change for the entire system between readings
		systemDelta = float64(v.CPUStats.SystemUsage) - float64(v.PreCPUStats.SystemUsage)
		onlineCPUs  = float64(v.CPUStats.OnlineCPUs)
	)

	// fallback for when onlineCPUs failed
	if onlineCPUs == 0.0 {
		onlineCPUs = float64(len(v.CPUStats.CPUUsage.PercpuUsage))
	}

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}
	return cpuPercent
}

func calcBlockIO(blkio types.BlkioStats) (float64, float64) {
	var blkRead, blkWrite uint64
	for _, entry := range blkio.IoServiceBytesRecursive {
		if len(entry.Op) == 0 {
			continue
		}
		switch entry.Op[0] {
		case 'r', 'R':
			blkRead += entry.Value
		case 'w', 'W':
			blkWrite += entry.Value
		}
	}
	return float64(blkRead), float64(blkWrite)
}

func calcMemUsageUnixNoCache(mem types.MemoryStats) float64 {
	// cgroup v1
	if v, isCgroup1 := mem.Stats["total_inactive_file"]; isCgroup1 && v < mem.Usage {
		return float64(mem.Usage - v)
	}
	// cgroup v2
	if v := mem.Stats["inactive_file"]; v < mem.Usage {
		return float64(mem.Usage - v)
	}
	return float64(mem.Usage)
}

func calcMemPercentUnixNoCahce(usedNoCache, limit float64) float64 {
	if limit == 0 {
		return 0
	}
	return usedNoCache / limit * 100.0
}

func calcNetwork(network map[string]types.NetworkStats) (float64, float64) {
	var rx, tx float64
	for _, v := range network {
		rx += float64(v.RxBytes)
		tx += float64(v.TxBytes)
	}
	return rx, tx
}

func collectContainersStats(ctx context.Context, client *client.Client, metrics *metrics, interval int) {
	for {
		// get all containers
		containers, err := client.ContainerList(ctx, containertypes.ListOptions{})
		if err != nil {
			log.Error().Err(err)
			continue
		}

		for _, container := range containers {
			resp, err := client.ContainerStats(ctx, container.ID, false)
			if err != nil {
				log.Error().Err(err)
				continue
			}
			defer resp.Body.Close()

			dec := json.NewDecoder(resp.Body)

			var (
				v                      *types.StatsJSON
				memPercent, cpuPercent float64
				blkRead, blkWrite      float64
				memUsage, memLimit     float64
			)

			if err := dec.Decode(&v); err != nil {
				log.Error().Err(err)
				continue
			}

			daemonOsType = resp.OSType

			if daemonOsType != "windows" {
				cpuPercent = calcCPUPercentUnix(v)
				blkRead, blkWrite = calcBlockIO(v.BlkioStats)
				memUsage = calcMemUsageUnixNoCache(v.MemoryStats)
				memLimit = float64(v.MemoryStats.Limit)
				memPercent = calcMemPercentUnixNoCahce(memUsage, memLimit)
			} else {
				// TODO: do some shits for window
			}

			netRx, netTx := calcNetwork(v.Networks)

			metrics.cpuPercent.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(cpuPercent)
			metrics.blkRead.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(blkRead)
			metrics.blkWrite.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(blkWrite)
			metrics.memUsage.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(memUsage)
			metrics.memLimit.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(memLimit)
			metrics.memPercent.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(memPercent)
			metrics.netRx.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(netRx)
			metrics.netTx.With(
				prometheus.Labels{"name": container.Names[0], "id": container.ID}).Set(netTx)
		}
		time.Sleep(time.Duration(interval) * time.Millisecond)
	}
}

type metrics struct {
	cpuPercent *prometheus.GaugeVec
	blkRead    *prometheus.GaugeVec
	blkWrite   *prometheus.GaugeVec
	memUsage   *prometheus.GaugeVec
	memLimit   *prometheus.GaugeVec
	memPercent *prometheus.GaugeVec
	netRx      *prometheus.GaugeVec
	netTx      *prometheus.GaugeVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		cpuPercent: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_cpu_percent",
			Help: "CPU% usage of the container",
		}, []string{"name", "id"}),
		blkRead: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_blk_read",
			Help: "I/O read usage",
		}, []string{"name", "id"}),
		blkWrite: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_blk_write",
			Help: "I/O write usage",
		}, []string{"name", "id"}),
		memUsage: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_mem_usage",
			Help: "Memory used",
		}, []string{"name", "id"}),
		memLimit: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_mem_limit",
			Help: "Memory Limit",
		}, []string{"name", "id"}),
		memPercent: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_mem_percent",
			Help: "Memory used in %",
		}, []string{"name", "id"}),
		netRx: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_net_rx",
			Help: "Network I/O receiving",
		}, []string{"name", "id"}),
		netTx: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dockerstats_net_tx",
			Help: "Network I/O transfering",
		}, []string{"name", "id"}),
	}

	reg.MustRegister(m.cpuPercent)
	reg.MustRegister(m.blkRead)
	reg.MustRegister(m.blkWrite)
	reg.MustRegister(m.memUsage)
	reg.MustRegister(m.memLimit)
	reg.MustRegister(m.memPercent)
	reg.MustRegister(m.netRx)
	reg.MustRegister(m.netTx)
	return m
}

func main() {
	var (
		port     = flag.String("port", ":8080", "The port to listen on for prometheus HTTP requests.")
		interval = flag.Int("interval", 1000, "Interval for gathering docker stats in milliseconds")
	)
  flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	ctx := context.Background()
	client, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err)
	}
	defer client.Close()

	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	go collectContainersStats(ctx, client, m, *interval)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	log.Info().Msgf("HTTP server started at " + *port)
	log.Fatal().Err(http.ListenAndServe(*port, nil))
}
