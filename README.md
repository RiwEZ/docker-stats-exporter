# Docker Stats Exporter for Prometheus
Simple exporter for `docker stats` command to prometheus written in Go.

## How to use?
Build it yourself by

```go build -o <binary_name> main.go```  

and put this into the server that you wish to gather docker stats and use it with

```
<binary_name> --port ":<port>" --interval <update_interval>
```

## Grafana Dashboard Example
You can try importing `grafana.json` for below dashboard example.
<img width="full" alt="image" src="https://github.com/RiwEZ/docker-stats-exporter/assets/55591062/342d15ba-179d-450d-b14e-496154c97b98">

## Refs 
- https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
- https://github.com/wywywywy/docker_stats_exporter
- https://github.com/prometheus/client_golang/blob/main/examples
