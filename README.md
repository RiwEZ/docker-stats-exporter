# Docker Stats Exporter for Prometheus
Simple exporter for `docker stats` command to prometheus written in Go.

# How to use?
Build it yourself by
```go build -o <binary_name> main.go```  
and put this into the server that you wish to gather docker stats (need to have docker installed)

# Refs 
- https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
- https://github.com/wywywywy/docker_stats_exporter
- https://github.com/prometheus/client_golang/blob/main/examples
