OTEL Demo
=========

This is a demo of how to use Open Telemetry (OTEL) instrumentation for traces
and metrics.


Usage
-----

Run Tempo, Tempo Web UI and the App frontend/backed via Docker Compose:

```shell
docker-compose up
```

Query the `main` endpoint:

```shell
curl -v localhost:8080/main
```

Query the `metrics` endpoint:

```shell
curl -v localhost:8080/metrics
curl -v localhost:8888/metrics
```


Author
------

Jiri Tyr
