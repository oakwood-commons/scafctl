# PowerShell examples for the scafctl REST API server.
# Start the server first: scafctl serve

$Base = "http://localhost:8080"

Write-Host "=== Root ==="
Invoke-RestMethod -Uri "$Base/"

Write-Host "`n`n=== Health ==="
Invoke-RestMethod -Uri "$Base/health"

Write-Host "`n`n=== Liveness ==="
Invoke-RestMethod -Uri "$Base/health/live"

Write-Host "`n`n=== Readiness ==="
Invoke-RestMethod -Uri "$Base/health/ready"

Write-Host "`n`n=== Metrics ==="
(Invoke-WebRequest -Uri "$Base/metrics").Content.Split("`n") | Select-Object -First 20

Write-Host "`n`n=== Providers ==="
Invoke-RestMethod -Uri "$Base/v1/providers"

Write-Host "`n`n=== Providers (filtered) ==="
Invoke-RestMethod -Uri "$Base/v1/providers?filter=item.name==%22file%22"

Write-Host "`n`n=== Eval CEL ==="
Invoke-RestMethod -Uri "$Base/v1/eval/cel" -Method Post `
  -ContentType "application/json" `
  -Body '{"expression": "1 + 2 + 3"}'

Write-Host "`n`n=== Eval Template ==="
Invoke-RestMethod -Uri "$Base/v1/eval/template" -Method Post `
  -ContentType "application/json" `
  -Body '{"template": "Hello, {{.name}}!", "data": {"name": "World"}}'

Write-Host "`n`n=== Config ==="
Invoke-RestMethod -Uri "$Base/v1/config"

Write-Host "`n`n=== Admin Info ==="
Invoke-RestMethod -Uri "$Base/v1/admin/info"

Write-Host "`n`n=== OpenAPI Spec ==="
(Invoke-WebRequest -Uri "$Base/v1/openapi.json").Content.Split("`n") | Select-Object -First 20
