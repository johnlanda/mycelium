<!-- Draft reference — verify against source -->

# ClientTrafficPolicy YAML (Envoy Gateway v1.3.0)

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: ClientTrafficPolicy
metadata:
  name: example-ctp
  namespace: default
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: eg

  http3: {}

  headers:
    xForwardedClientCert:
      mode: SanitizeSet
      certDetailsToAdd:
        - Subject
        - Cert
        - DNS

    withUnderscoresAction: RejectRequest

    disableRateLimitHeaders: true

    earlyRequestHeaders:
      set:
        - name: X-Request-Start
          value: "%START_TIME%"

  tcpKeepalive:
    idleTime: 60s
    interval: 30s
    probes: 3
```

## Key Go Type References

- **ClientTrafficPolicySpec** (`clienttrafficpolicy_types.go`):
  - `HTTP3 *HTTP3Settings` — empty struct enables HTTP/3
  - `Headers *HeaderSettings` — header manipulation
  - `TCPKeepalive *TCPKeepalive` — keepalive settings

- **HeaderSettings**:
  - `XForwardedClientCert *XForwardedClientCert` with `Mode XFCCForwardMode`
  - `WithUnderscoresAction *WithUnderscoresAction` enum: Allow, RejectRequest, DropHeader
  - `DisableRateLimitHeaders *bool`
  - `EarlyRequestHeaders *gwapiv1.HTTPHeaderFilter`

- **XFCCForwardMode** enum: Sanitize, ForwardOnly, AppendForward, SanitizeSet, AlwaysForwardOnly
- **TCPKeepalive**: `IdleTime`, `Interval` (Duration), `Probes` (*uint32)
