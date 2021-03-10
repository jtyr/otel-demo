OTEL Demo
=========

This is a demo of how to use Open Telemetry (OTEL) instrumentation for traces
and metrics.


Usage
-----

### Docker compose

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

### Kubernetes

Install local Kubernetes cluster using K3D:

```shell
export KUBECONFIG=~/.kube/kind_test1
k3d cluster create test1 -p '80:80@loadbalancer' -p '443:443@loadbalancer'
```

Add all required Helm repos:

```shell
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add otel-demo https://jtyr.github.io/otel-demo
helm repo update
```

Install Prometheus Operator Stack:

```shell
cat <<END | helm upgrade --create-namespace --namespace prometheus --values - --install kps prometheus-community/kube-prometheus-stack
fullnameOverride: kps
prometheus:
  ingress:
    enabled: true
    hosts:
      - prometheus.localhost
alertmanager:
  ingress:
    enabled: true
    hosts:
      - alertmanager.localhost
grafana:
  enabled: false
END
```

Install Grafana:

```shell
cat <<END | helm upgrade --create-namespace --namespace grafanalabs --values - --install grafana grafana/grafana
adminPassword: admin
ingress:
  enabled: true
  hosts:
    - grafana.localhost
datasources:
  datasources.yaml:
    apiVersion: 1
    datasources:
      - name: Prometheus
        uid: prometheus
        type: prometheus
        url: http://kps-prometheus.prometheus:9090
        access: proxy
        isDefault: true
      - name: Loki
        uid: loki
        type: loki
        url: http://loki:3100
        access: proxy
        jsonData:
          derivedFields:
            - name: "traceID"
              matcherRegex: "traceID=(\\\\w+)"
              url: "\$\${__value.raw}"
              datasourceUid: tempo
      - name: Tempo
        uid: tempo
        type: tempo
        url: http://tempo:16686
        access: proxy
dashboardProviders:
  dashboardproviders.yaml:
    apiVersion: 1
    providers:
      - name: default
        options:
          path: /var/lib/grafana/dashboards/default
dashboards:
  default:
    local-dashboard:
      url: https://raw.githubusercontent.com/jtyr/otel-demo/master/files/dashboard.json
END
```

Install Grafana Tempo:

```shell
helm upgrade --create-namespace --namespace grafanalabs --install tempo grafana/tempo
cat <<END | helm upgrade --create-namespace --namespace grafanalabs --values - --install loki grafana/loki
tracing:
  jaegerAgentHost: tempo
END
# Patch tempo to have Jaeger HTTP port available for writes
kubectl patch sts tempo --type json --patch '[{"op": "add", "path": "/spec/template/spec/containers/0/ports/-", "value": {"containerPort": 14268, "name": "jaeger-http"}}]'
```

Install Grafana Promtail:

```shell
helm upgrade --create-namespace --namespace grafanalabs --install promtail grafana/promtail
```

Install Fluent Bit:

```shell
cat <<END | helm upgrade --create-namespace --namespace grafanalabs --values - --install fluent-bit grafana/fluent-bit
loki:
  serviceName: loki.grafanalabs
END
```

Install OTEL Demo:

```shell
helm upgrade --create-namespace --namespace otel-demo --install otel-demo-backend otel-demo/otel-demo-backend
helm upgrade --create-namespace --namespace otel-demo --install otel-demo-frontend otel-demo/otel-demo-frontend
```

Test the app:

- [Grafana dashboard](http://grafana.localhost/d/otel-demo/otel-demo)
- [OTEL Demo - frontend](http://otel-demo-frontend.localhost)
- [OTEL Demo - frontend metrics](http://otel-demo-frontend.localhost/metrics)


Author
------

Jiri Tyr
