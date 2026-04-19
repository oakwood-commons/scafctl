// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package stateprovider

// This file is intentionally minimal -- the state provider has no external
// dependencies to mock. It operates entirely on in-memory StateData from context.
// Tests use state.NewMockStateData() and state.WithState() directly.
