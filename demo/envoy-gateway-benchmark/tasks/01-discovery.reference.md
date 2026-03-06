<!-- Draft reference — verify against source -->

# SecurityPolicy Authentication Methods (Envoy Gateway v1.3.0)

The `SecurityPolicySpec` struct in `api/v1alpha1/securitypolicy_types.go` defines these security fields:

## 1. API Key Authentication — `APIKeyAuth`
**Go type:** `APIKeyAuth` in `api/v1alpha1/api_key_auth_types.go`

```go
const APIKeysSecretKey = "credentials"

type APIKeyAuth struct {
    CredentialRefs []gwapiv1.SecretObjectReference `json:"credentialRefs"`
    ExtractFrom    []*ExtractFrom                  `json:"extractFrom"`
}

type ExtractFrom struct {
    Headers []string `json:"headers,omitempty"`
    Params  []string `json:"params,omitempty"`
    Cookies []string `json:"cookies,omitempty"`
}
```

- Keys stored in **Kubernetes Opaque Secrets** referenced by `CredentialRefs`
- Each secret maps client ID (key name) to API key value (key value)
- `ExtractFrom` defines where to look: HTTP headers, URL query parameters, or cookies
- Multiple `ExtractFrom` entries evaluated in order; first match wins

## 2. Basic Authentication — `BasicAuth`
**Go type:** `BasicAuth` in `api/v1alpha1/basic_auth_types.go`

- `Users` field: SecretObjectReference to .htpasswd-formatted secret
- Constant: `BasicAuthUsersSecretKey = ".htpasswd"`

## 3. JWT Authentication — `JWT`
**Go type:** `JWT` in `api/v1alpha1/jwt_types.go`

- `Optional *bool` — whether missing JWT is acceptable
- `Providers []JWTProvider` — 1-4 JWT providers with JWKS, audiences, claim extraction

## 4. OIDC Authentication — `OIDC`
**Go type:** `OIDC` in `api/v1alpha1/oidc_types.go`

- `Provider OIDCProvider` — issuer, authorization/token endpoints
- `ClientID`, `ClientSecret` (secret key: `"client-secret"`)
- Cookie configuration, scopes, redirect URL, token refresh

## 5. External Authorization — `ExtAuth`
**Go type:** `ExtAuth` in `api/v1alpha1/ext_auth_types.go`

- Supports gRPC (`GRPCExtAuthService`) or HTTP (`HTTPExtAuthService`)
- Mutually exclusive: must specify exactly one
- Headers, body forwarding, status on error

## 6. Authorization — `Authorization`
**Go type:** `Authorization` in `api/v1alpha1/authorization_types.go`

- `Rules []AuthorizationRule` — first-match evaluation
- `DefaultAction` — Allow or Deny when no rule matches
- Rules support `Principal` (clientCIDRs, JWTPrincipal) and action (Allow/Deny)

## 7. CORS — `CORS`
**Go type:** `CORS` in `api/v1alpha1/cors_types.go`

- AllowOrigins, AllowMethods, AllowHeaders, ExposeHeaders, MaxAge
