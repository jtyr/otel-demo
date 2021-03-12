OTEL Demo
=========

This is a demo of how to use [Open Telemetry](https://opentelemetry.io/) (OTEL)
instrumentation for traces and metrics.


Usage
-----

### Docker compose

Run Grafana Tempo, Grafana Tempo Web UI and the App frontend/backed via Docker
Compose:

```shell
docker-compose up
```

Query the `main` endpoint:

```shell
curl http://localhost:8080
```

Query the `metrics` endpoint:

```shell
curl http://localhost:8080/metrics
curl http://localhost:8888/metrics
```

### Kubernetes

Install local Kubernetes cluster using [K3D](https://k3d.io):

```shell
export KUBECONFIG=~/.kube/kind_test1
k3d cluster create test1 -p '80:80@loadbalancer' -p '443:443@loadbalancer'
```

Add all required [Helm](https://helm.sh) repos:

```shell
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add otel-demo https://jtyr.github.io/otel-demo
helm repo update
```

Install [Kube Prometheus Stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack):

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

Install [Grafana](https://grafana.com/oss/grafana/):

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
        editable: true
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
        editable: true
      - name: Tempo
        uid: tempo
        type: tempo
        url: http://tempo:16686
        access: proxy
        editable: true
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

Install [Grafana Tempo](https://grafana.com/oss/tempo/):

```shell
helm upgrade --create-namespace --namespace grafanalabs --install tempo grafana/tempo
```

Install [Grafana Loki](https://grafana.com/oss/loki/):

```shell
cat <<END | helm upgrade --create-namespace --namespace grafanalabs --values - --install loki grafana/loki
tracing:
  jaegerAgentHost: tempo
ingress:
  enabled: true
  hosts:
    - host: loki.localhost
      paths:
        - /
END
```

Install [Grafana
Promtail](https://grafana.com/docs/loki/latest/clients/promtail/):

```shell
cat <<END | helm upgrade --create-namespace --namespace grafanalabs --values - --install promtail grafana/promtail
config:
  lokiAddress: http://loki:3100/loki/api/v1/push
END
```

Install [Fluent Bit](https://fluentbit.io):

```shell
cat <<END | helm upgrade --create-namespace --namespace grafanalabs --values - --install fluent-bit grafana/fluent-bit
loki:
  serviceName: loki.grafanalabs
END
```

Install [OTEL Demo](https://github.com/jtyr/otel-demo):

```shell
helm upgrade --create-namespace --namespace otel-demo --install otel-demo-backend otel-demo/otel-demo-backend
helm upgrade --create-namespace --namespace otel-demo --install otel-demo-frontend otel-demo/otel-demo-frontend
```

Test the app:

- [Grafana dashboard](http://grafana.localhost/d/otel-demo/otel-demo)
- [OTEL Demo - frontend](http://otel-demo-frontend.localhost)
- [OTEL Demo - frontend metrics](http://otel-demo-frontend.localhost/metrics)

Query logs from command line:

```shell
export LOKI_ADDR=http://loki.localhost
logcli query -t '{namespace="otel-demo", instance=~"otel-demo-.*"}'
```

Check and tune error generation of the `backend`:

```shell
# Query the current value
kubectl run curl \
    --image curlimages/curl \
    --restart=Never \
    --rm \
    --tty \
    --stdin \
    --command -- \
    curl http://otel-demo-backend.otel-demo/api/features/errorGenerator
# Set a new value
# (-d parameter is a number of miliseconds; 0 = generator disabled)
kubectl run curl \
    --image curlimages/curl \
    --restart=Never \
    --rm \
    --tty \
    --stdin \
    --command -- \
    curl -X PUT -d 10 http://otel-demo-backend.otel-demo/api/features/errorGenerator
```


Author
------

Jiri Tyr
