<!-- Draft reference — verify against source -->

# Backend + BackendTrafficPolicy Manifests (Envoy Gateway v1.3.0)

## 1. Backend: Primary API endpoint

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: Backend
metadata:
  name: api-backend
  namespace: default
spec:
  endpoints:
    - fqdn:
        hostname: api.example.com
        port: 443
```

## 2. Backend: Fallback endpoint

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: Backend
metadata:
  name: fallback-api-backend
  namespace: default
spec:
  endpoints:
    - fqdn:
        hostname: fallback-api.example.com
        port: 443
```

## 3. BackendTrafficPolicy: Circuit breaking + retry

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: BackendTrafficPolicy
metadata:
  name: api-traffic-policy
  namespace: default
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: api-route
  circuitBreaker:
    maxConnections: 100
    maxPendingRequests: 50
  retry:
    numRetries: 3
    retryOn:
      triggers:
        - "5xx"
        - "gateway-error"
    perRetry:
      backOff:
        baseInterval: 500ms
        maxInterval: 5s
```

## Key field details (from v1.3.0 Go types)

- `Backend.spec.endpoints[].fqdn` uses `FQDNEndpoint` with `hostname` + `port`
- `circuitBreaker` fields: `maxConnections`, `maxPendingRequests`, `maxParallelRequests`, `maxParallelRetries`, `maxRequestsPerConnection`
- `retry.retryOn.triggers` accepts `TriggerEnum` values: `"5xx"`, `"gateway-error"`, `"connect-failure"`, `"retriable-status-codes"`, `"reset"`
- `retry.perRetry.backOff` has `baseInterval` and `maxInterval`
- `targetRefs` (plural array) is the current field; singular `targetRef` is deprecated
