<!-- Draft reference — verify against source -->

# EnvoyExtensionPolicy: ext_proc Configuration (Envoy Gateway v1.3.0)

## YAML Manifest

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyExtensionPolicy
metadata:
  name: auth-enrichment
  namespace: ext-proc-system
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: backend
  extProc:
    - backendRefs:
        - group: ""
          kind: Service
          name: auth-enrichment
          namespace: ext-proc-system
          port: 9001
      messageTimeout: 500ms
      failOpen: true
      processingMode:
        request:
          body: Buffered
        response:
          body: NotSent
      metadataOptions:
        receivingNamespaces:
          untyped:
            - envoy.filters.http.jwt_authn
        writableNamespaces:
          untyped:
            - com.example.auth
```

## Key types from `api/v1alpha1/ext_proc_types.go`:

- `ExtProc` — top-level array of ext_proc configurations
- `BackendRefs []BackendRef` — service references
- `MessageTimeout *metav1.Duration` — processing timeout
- `FailOpen *bool` — allow traffic if ext_proc fails
- `ProcessingMode` with `Request`/`Response` sub-structs containing `Body` field

## Three Body Processing Modes

| Mode | Behavior |
|------|----------|
| **Streamed** | Body chunks forwarded to ext_proc as they arrive. Proxy doesn't wait for response before forwarding next chunk. Suitable for large bodies (file uploads) where buffering is impractical. |
| **Buffered** | Proxy collects the entire body and sends it as a single message with `end_of_stream: true`. Upstream/downstream paused until service responds. Best for decisions requiring full body (JSON validation, signing). |
| **BufferedPartial** | Like Buffered but with a size limit. If body exceeds buffer limit, collected portion sent with `end_of_stream: false` and rest flows through unexamined. Good for inspecting first N bytes (content sniffing). |

In this policy: `Buffered` for requests (auth service needs full body), `NotSent` for responses (no need to inspect response bodies).
