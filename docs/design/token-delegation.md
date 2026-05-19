# Token Delegator: Architectural Breakdown

## 1. Purpose / Intent (I/O)

**Input:** A request context containing a validated caller token + a desired downstream scope.

**Output:** A valid access token scoped to the downstream resource, representing the original caller's identity.

The Delegator is the single entry point that answers: "Give me a token I can use to call this downstream API on behalf of whoever called me."

## 2. Variability (What May Change)

| Dimension | What Varies | Azure (WIF-only, this increment) |
|-----------|-------------|----------------------------------|
| Grant Type | OBO vs. client_credentials vs. token_exchange vs. pass-through | OBO for users, client_credentials for apps |
| Server Auth Method | WIF file, client secret, cert assertion | WIF file only |
| Token Endpoint | URL shape differs per provider | `https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token` |
| Request Body Shape | Field names, encoding, optional params | `url.Values` form POST |
| Caller Identity Signal | How to determine user vs. app | `idtyp` claim from `AuthClaims` |
| Scope Validation | What scopes are allowed | Static allowlist from config |
| Token Response Shape | JSON field names for access_token, expires_in | Standard OAuth2 response |

## 3. Core Components (Data Model)

| Component | Responsibility |
|-----------|---------------|
| **Delegator** | Facade — routes to the correct strategy based on caller type |
| **Strategy** | Knows how to build one specific grant type request |
| **ServerCredential** | Knows how to prove the server's identity to the token endpoint |
| **TokenResponse** | Normalized result from any strategy |
| **ScopePolicy** | Decides whether a requested scope is allowed |

## 4. Policies (Rules)

- If caller is `user` → use OBO strategy
- If caller is `app` → use client_credentials strategy
- If requested scope is not in `AllowedOBOScopes` → reject
- If no caller token in context → error (not in API mode)
- If WIF token file is unreadable → error (no fallback in this increment)
- Token endpoint returns non-200 → propagate error with detail

## 5. Mechanisms (How Policies Are Carried Out)

- **Caller type determination:** `middleware.ClaimsFromContext(ctx).CallerType()` → `"user"` or `"app"`
- **Caller token extraction:** `middleware.AccessTokenFromContext(ctx)`
- **Server credential loading:** `os.ReadFile(federatedTokenFile)` per request (K8s rotates)
- **Request construction:** `url.Values` with grant-type-specific fields
- **Scope validation:** Set lookup against `allowedScopes map[string]bool`
- **Response parsing:** JSON decode into normalized struct

## 6. Types (Interfaces, Functional Types, Structs)

~~~go
// ── Interface: where strategy varies ──

// TokenDelegator is the top-level facade. One per server.
type TokenDelegator interface {
    DelegateToken(ctx context.Context, scope string) (*DelegatedToken, error)
}

// GrantStrategy builds a token request for a specific grant type.
// This is the variability point for OBO vs. client_credentials vs. future exchange types.
type GrantStrategy interface {
    Execute(ctx context.Context, params GrantParams) (*DelegatedToken, error)
}

// ServerCredential provides the server's proof-of-identity for the token endpoint.
// This is the variability point for WIF vs. secret vs. cert.
type ServerCredential interface {
    Apply(params url.Values) error
}

// ── Functional Type: caller type → strategy selection ──

// StrategySelector picks a GrantStrategy based on the caller context.
type StrategySelector func(callerType string) GrantStrategy

// ── Structs: concrete data ──

// DelegatedToken is the normalized output of any delegation strategy.
type DelegatedToken struct {
    AccessToken string
    TokenType   string
    ExpiresIn   int
    Scope       string
}

// GrantParams holds the per-request inputs a strategy needs to build its request.
type GrantParams struct {
    CallerToken string   // the inbound bearer token (assertion for OBO)
    Scope       string   // desired downstream scope
}

// EntraDelegatorConfig holds the static configuration for the Entra delegator.
type EntraDelegatorConfig struct {
    TenantID           string
    ClientID           string
    FederatedTokenFile string
    AllowedScopes      []string
}

// EntraDelegator implements TokenDelegator for Azure/Entra.
// Constructed once at server startup.
type EntraDelegator struct {
    tokenURL       string              // pre-built: https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
    clientID       string              // pre-set from config
    credential     ServerCredential    // WIF in this increment
    selectStrategy StrategySelector    // routes caller type → grant strategy
    allowedScopes  map[string]bool     // pre-computed from config
    client         *http.Client        // reused across requests
}

// WIFCredential implements ServerCredential by reading a projected token file.
type WIFCredential struct {
    TokenFile           string // path to federated token (re-read per request)
    ClientAssertionType string // constant: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
}

// OBOStrategy implements GrantStrategy for the OBO flow (user callers).
type OBOStrategy struct {
    tokenURL   string
    clientID   string
    credential ServerCredential
    client     *http.Client
}

// ClientCredentialsStrategy implements GrantStrategy for app callers.
type ClientCredentialsStrategy struct {
    tokenURL   string
    clientID   string
    credential ServerCredential
    client     *http.Client
}
~~~

## 7. Data Flow (Request Journey Through the Delegator)

~~~
1. Provider calls DelegateToken(ctx, "https://graph.microsoft.com/.default")
        │
2. EntraDelegator.DelegateToken:
   ├─ Extract callerToken = AccessTokenFromContext(ctx)
   ├─ If callerToken == "" → error: "no caller token"
   ├─ Validate scope against allowedScopes
   │   └─ If not allowed → error: "scope not permitted"
   ├─ callerType = ClaimsFromContext(ctx).CallerType()
   └─ strategy = selectStrategy(callerType)
        │
3. Strategy selected:
   ├─ "user" → OBOStrategy
   └─ "app"  → ClientCredentialsStrategy
        │
4. strategy.Execute(ctx, GrantParams{CallerToken, Scope}):
   ├─ Build url.Values with grant-type-specific fields
   ├─ credential.Apply(params):
   │   └─ WIFCredential: ReadFile(tokenFile) → set client_assertion + client_assertion_type
   ├─ POST tokenURL with params
   ├─ Parse JSON response
   └─ Return DelegatedToken{AccessToken, TokenType, ExpiresIn, Scope}
        │
5. Provider receives DelegatedToken, sets Authorization header on downstream request
~~~
