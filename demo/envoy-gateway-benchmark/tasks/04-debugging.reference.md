<!-- Draft reference — verify against source -->

# Authorization Rule Evaluation Bug

## Root Cause: First-Match Semantics

The request from `10.1.2.3` without a JWT is allowed because of **first-match rule evaluation** in SecurityPolicy's authorization rules.

## What Happens

```
Rule 1: allow-internal
  Principal: SourceIP = 10.0.0.0/8  -> MATCHES (10.1.2.3 is in range)
  Action: Allow
  -> Evaluation stops here, request ALLOWED

Rule 2: allow-jwt-admin
  (never evaluated because Rule 1 already matched)

Rule 3: deny-external
  (never evaluated because Rule 1 already matched)
```

## Rule Evaluation Model

From `authorization_types.go`:

- `Authorization.Rules []AuthorizationRule` — evaluated in order, first match wins
- `Authorization.DefaultAction` — applied when no rule matches
- Within a single rule: `Principal` conditions are ANDed with the action
- Across rules: first-match (NOT evaluated as a whole)

## Fix: Combine conditions in a single rule with defaultAction: Deny

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: combined-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
  authorization:
    defaultAction: Deny
    rules:
    - name: require-internal-and-jwt-admin
      action: Allow
      principal:
        clientCIDRs:
          - addressPrefix: "10.0.0.0"
            prefixLength: 8
        jwt:
          provider: my-jwt-provider
          claims:
            - name: role
              values: ["admin"]
```

This ensures BOTH internal IP AND valid JWT with admin role are required.
