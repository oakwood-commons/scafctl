---
description: "Check if staged changes have corresponding tests, examples, and documentation."
agent: "agent"
argument-hint: "Optional: specific area to check"
---
Review staged changes and check if supporting artifacts exist:

1. Run `git diff --cached --stat` to identify staged changes
2. If nothing is staged, fall back to `git log origin/main..HEAD --stat` to check pushed commits on the branch
3. For each feature, type, or interface change, verify:
   - Tests in corresponding `*_test.go` files
   - Benchmarks for performance-sensitive code
   - Examples in `examples/` if user-facing
   - Updated README or doc comments on exported types
   - Integration tests (`TestIntegration_*`) if cross-package
   - Mock updates in `testutil/` if interfaces changed
4. Report present vs missing as a checklist
5. Do not create anything, just report the gaps
