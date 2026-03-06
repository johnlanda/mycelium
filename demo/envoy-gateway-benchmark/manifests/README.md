# Envoy Gateway v1.3.0 Manifests

This directory contains Kubernetes YAML manifests for Envoy Gateway v1.3.0, demonstrating Backend resources with FQDN endpoints, fallback configuration, circuit breaking, and retry logic with exponential backoff.

## Files

### 1. backends.yaml

Defines two Backend resources using FQDN endpoints:

#### Primary Backend (`api-backend`)
- **Hostname**: `api.example.com:443`
- **Type**: FQDN endpoint (Fully Qualified Domain Name)
- **Purpose**: Primary API backend for routing traffic

#### Fallback Backend (`fallback-api-backend`)
- **Hostname**: `fallback-api.example.com:443`
- **Type**: FQDN endpoint
- **Fallback**: `true` - Receives traffic when primary backend health falls below 72%
- **Purpose**: Backup API backend for high availability

**Key Features:**
- Uses `apiVersion: gateway.envoyproxy.io/v1alpha1`
- `kind: Backend` - Envoy Gateway custom resource
- FQDN endpoints via `spec.endpoints[].fqdn` with `hostname` and `port`
- Fallback configuration with overprovisioning factor of 1.4

### 2. backend-traffic-policy.yaml

Defines a BackendTrafficPolicy with comprehensive traffic management:

#### Circuit Breaker Configuration
Controls connection and request limits to prevent backend overload:

- **maxConnections**: 100
  - Limits total connections to the backend
  - New connections queued when threshold is met

- **maxPendingRequests**: 50
  - Limits queued requests waiting for connections
  - Requests terminated with 503 when exceeded

- **maxParallelRequests**: 1024 (default)
  - Limits concurrent in-flight requests

- **maxParallelRetries**: 1024 (default)
  - Limits concurrent retry attempts

#### Retry Configuration
Handles transient failures with intelligent retry logic:

- **numRetries**: 3 attempts
  - Retries the request up to 3 additional times

- **Retry Triggers**:
  - `5xx` - Any 5xx server error
  - `gateway-error` - Gateway errors (502, 503, 504)
  - `connect-failure` - Connection timeouts
  - `refused-stream` - Stream refused by upstream
  - `reset` - Connection reset
  - `retriable-status-codes` - Specific HTTP status codes

- **HTTP Status Codes**: 503, 504
  - Specific codes that trigger retries (requires `retriable-status-codes` trigger)

#### Exponential Backoff
Implements jittered exponential backoff to prevent thundering herd:

- **baseInterval**: 500ms
  - Starting delay for first retry

- **maxInterval**: 10s
  - Maximum delay between retries (caps exponential growth)

- **Retry Timing** (with jitter):
  - 1st retry: ~500ms after failure
  - 2nd retry: ~1000ms after 1st retry
  - 3rd retry: ~2000ms after 2nd retry

- **perRetry.timeout**: 10s
  - Timeout for each individual retry attempt

## Usage

### Prerequisites

1. Envoy Gateway v1.3.0 installed in your cluster
2. A Gateway and HTTPRoute configured (see example below)

### Apply the Manifests

```bash
# Apply Backend resources
kubectl apply -f manifests/backends.yaml

# Apply BackendTrafficPolicy
kubectl apply -f manifests/backend-traffic-policy.yaml
```

### Example HTTPRoute Integration

To use these Backend resources, you need an HTTPRoute that references them:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api-route
  namespace: default
spec:
  parentRefs:
    - name: eg  # Your Gateway name
  hostnames:
    - "api.example.com"
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        # Primary backend reference
        - group: gateway.envoyproxy.io
          kind: Backend
          name: api-backend
          port: 443
        # Fallback backend reference
        - group: gateway.envoyproxy.io
          kind: Backend
          name: fallback-api-backend
          port: 443
```

### Verification

Check Backend status:
```bash
kubectl get backends
kubectl describe backend api-backend
kubectl describe backend fallback-api-backend
```

Check BackendTrafficPolicy status:
```bash
kubectl get backendtrafficpolicies
kubectl describe backendtrafficpolicy api-traffic-policy
```

View Envoy Proxy stats:
```bash
# Check retry statistics
egctl x stats envoy-proxy -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-name=eg \
  | grep "envoy_cluster_upstream_rq_retry"

# Check circuit breaker statistics
egctl x stats envoy-proxy -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-name=eg \
  | grep "envoy_cluster_circuit_breakers"
```

## Architecture Notes

### Backend Resource
- **apiVersion**: `gateway.envoyproxy.io/v1alpha1`
- **kind**: `Backend`
- Supports three endpoint types:
  - `fqdn` - Fully Qualified Domain Name (DNS resolution)
  - `ip` - Static IP address (IPv4 or IPv6)
  - `unix` - Unix domain socket path
- Only one endpoint type can be used per Backend
- Multiple FQDN endpoints in a Backend must all use the same type (cannot mix FQDN with IP)

### BackendTrafficPolicy
- **apiVersion**: `gateway.envoyproxy.io/v1alpha1`
- **kind**: `BackendTrafficPolicy`
- Can target: Gateway, HTTPRoute, or GRPCRoute resources
- Circuit breakers are per-BackendReference (separate counters for each backend)
- Retry triggers follow Envoy Proxy retry semantics
- Exponential backoff uses fully jittered algorithm to prevent thundering herd

### Fallback Behavior
- Fallback backends use overprovisioning factor of 1.4
- Fallback receives traffic when active backend health < 72% (1/1.4 ≈ 0.72)
- Highly recommended to configure active or passive health checks
- Automatic readjustment when primary backends become healthy again

## Best Practices

1. **Health Checks**: Always configure health checks when using fallback backends
2. **Circuit Breaker Tuning**: Default threshold (1024) may be too high for high-throughput systems
3. **Retry Budget**: Limit retries to prevent retry storms (use `maxParallelRetries`)
4. **Exponential Backoff**: Always use exponential backoff to prevent thundering herd
5. **Timeout Configuration**: Set `perRetry.timeout` lower than overall request timeout
6. **Monitoring**: Monitor circuit breaker and retry stats to tune thresholds

## References

- [Envoy Gateway v1.3.0 Documentation](https://gateway.envoyproxy.io/v1.3/)
- [Backend API Reference](https://gateway.envoyproxy.io/v1.3/api/extension_types/#backend)
- [BackendTrafficPolicy API Reference](https://gateway.envoyproxy.io/v1.3/api/extension_types/#backendtrafficpolicy)
- [Circuit Breaker Guide](https://gateway.envoyproxy.io/v1.3/tasks/traffic/circuit-breaker/)
- [Retry Guide](https://gateway.envoyproxy.io/v1.3/tasks/traffic/retry/)
- [Envoy Circuit Breakers](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/circuit_breaking)
- [Envoy Retry Configuration](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/router_filter#x-envoy-retry-on)
