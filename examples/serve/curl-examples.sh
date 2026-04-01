#!/usr/bin/env bash
# curl examples for the scafctl REST API server.
# Start the server first: scafctl serve

BASE="http://localhost:8080"

echo "=== Root ==="
curl -s "$BASE/"

echo -e "\n\n=== Health ==="
curl -s "$BASE/health"

echo -e "\n\n=== Liveness ==="
curl -s "$BASE/health/live"

echo -e "\n\n=== Readiness ==="
curl -s "$BASE/health/ready"

echo -e "\n\n=== Metrics ==="
curl -s "$BASE/metrics" | head -20

echo -e "\n\n=== Providers ==="
curl -s "$BASE/v1/providers"

echo -e "\n\n=== Providers (filtered) ==="
curl -s "$BASE/v1/providers?filter=item.name==%22file%22"

echo -e "\n\n=== Eval CEL ==="
curl -s -X POST "$BASE/v1/eval/cel" \
  -H "Content-Type: application/json" \
  -d '{"expression": "1 + 2 + 3"}'

echo -e "\n\n=== Eval Template ==="
curl -s -X POST "$BASE/v1/eval/template" \
  -H "Content-Type: application/json" \
  -d '{"template": "Hello, {{.name}}!", "data": {"name": "World"}}'

echo -e "\n\n=== Config ==="
curl -s "$BASE/v1/config"

echo -e "\n\n=== Admin Info ==="
curl -s "$BASE/v1/admin/info"

echo -e "\n\n=== OpenAPI Spec ==="
curl -s "$BASE/v1/openapi.json" | head -20
