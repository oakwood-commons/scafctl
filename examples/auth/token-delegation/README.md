# Token Delegation Examples

These examples demonstrate configuring token delegation for the scafctl API server.

## Files

| File | Description |
|------|-------------|
| `entra-obo-config.yaml` | Entra ID OBO + client credentials delegation config |
| `passthrough-config.yaml` | Token pass-through config for GitHub and custom services |
| `delegation-solution.yaml` | Solution using delegated auth in HTTP provider calls |

## Quick Start

```bash
# Start API server with Entra delegation
SCAFCTL_API_ENTRA_CLIENT_SECRET="your-secret" \
  scafctl serve --config ./entra-obo-config.yaml

# Test OBO flow (caller's token is exchanged for downstream token)
curl -H "Authorization: Bearer <user-token>" \
     http://localhost:8080/v1/solutions/delegation-solution/run

# Test pass-through (caller provides downstream token directly)
curl -H "Authorization: Bearer <user-token>" \
     -H "X-Authorization-Github: ghp_xxxx" \
     http://localhost:8080/v1/solutions/delegation-solution/run
```

## See Also

- [Token Delegation Tutorial](../../../docs/tutorials/token-delegation-tutorial.md)
- [API Token Delegation Design](../../../docs/design/api-token-delegation.md)
