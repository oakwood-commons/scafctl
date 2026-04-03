// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelperGet(t *testing.T) {
	tests := []struct {
		name        string
		setupStore  func(*secrets.MockStore)
		setupNative func(t *testing.T) *catalog.NativeCredentialStore
		serverURL   string
		want        *Credential
		wantErr     bool
		errContains string
	}{
		{
			name: "found in credhelper namespace",
			setupStore: func(m *secrets.MockStore) {
				cred := Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "token1"}
				data, _ := json.Marshal(cred)
				m.Data["credhelper:https://ghcr.io"] = data
			},
			serverURL: "https://ghcr.io",
			want:      &Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "token1"},
		},
		{
			name:       "fallback to native credential store",
			setupStore: func(_ *secrets.MockStore) {},
			setupNative: func(t *testing.T) *catalog.NativeCredentialStore {
				t.Helper()
				ns := catalog.NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
				err := ns.SetCredential("ghcr.io", "nativeuser", "nativepass", "")
				require.NoError(t, err)
				return ns
			},
			serverURL: "https://ghcr.io",
			want:      &Credential{ServerURL: "https://ghcr.io", Username: "nativeuser", Secret: "nativepass"},
		},
		{
			name: "credhelper preferred over native",
			setupStore: func(m *secrets.MockStore) {
				cred := Credential{ServerURL: "https://ghcr.io", Username: "chuser", Secret: "chtoken"}
				data, _ := json.Marshal(cred)
				m.Data["credhelper:https://ghcr.io"] = data
			},
			setupNative: func(t *testing.T) *catalog.NativeCredentialStore {
				t.Helper()
				ns := catalog.NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
				err := ns.SetCredential("ghcr.io", "otheruser", "otherpass", "")
				require.NoError(t, err)
				return ns
			},
			serverURL: "https://ghcr.io",
			want:      &Credential{ServerURL: "https://ghcr.io", Username: "chuser", Secret: "chtoken"},
		},
		{
			name:        "empty server URL",
			setupStore:  func(_ *secrets.MockStore) {},
			serverURL:   "",
			wantErr:     true,
			errContains: "credentials not found",
		},
		{
			name:        "whitespace-only server URL",
			setupStore:  func(_ *secrets.MockStore) {},
			serverURL:   "   ",
			wantErr:     true,
			errContains: "credentials not found",
		},
		{
			name:        "unknown registry",
			setupStore:  func(_ *secrets.MockStore) {},
			serverURL:   "https://unknown.registry.io",
			wantErr:     true,
			errContains: "credentials not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := secrets.NewMockStore()
			tt.setupStore(store)

			opts := []Option{}
			if tt.setupNative != nil {
				opts = append(opts, WithNativeStore(tt.setupNative(t)))
			}

			helper := New(store, opts...)
			got, err := helper.Get(context.Background(), tt.serverURL)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHelperStore(t *testing.T) {
	tests := []struct {
		name        string
		cred        *Credential
		storeErr    error
		wantErr     bool
		errContains string
	}{
		{
			name: "store credential",
			cred: &Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "token1"},
		},
		{
			name:        "empty ServerURL rejected",
			cred:        &Credential{ServerURL: "", Username: "user1", Secret: "token1"},
			wantErr:     true,
			errContains: "ServerURL is required",
		},
		{
			name:     "store error propagated",
			cred:     &Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "token1"},
			storeErr: assert.AnError,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := secrets.NewMockStore()
			if tt.storeErr != nil {
				store.SetErr = tt.storeErr
			}

			helper := New(store)
			err := helper.Store(context.Background(), tt.cred)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)

			// Verify stored data
			data, getErr := store.Get(context.Background(), "credhelper:"+tt.cred.ServerURL)
			require.NoError(t, getErr)

			var got Credential
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, tt.cred.ServerURL, got.ServerURL)
			assert.Equal(t, tt.cred.Username, got.Username)
			assert.Equal(t, tt.cred.Secret, got.Secret)
		})
	}
}

func TestHelperStoreGetRoundTrip(t *testing.T) {
	store := secrets.NewMockStore()
	helper := New(store)
	ctx := context.Background()

	cred := &Credential{ServerURL: "https://ghcr.io", Username: "testuser", Secret: "testtoken"}
	require.NoError(t, helper.Store(ctx, cred))

	got, err := helper.Get(ctx, cred.ServerURL)
	require.NoError(t, err)
	assert.Equal(t, cred, got)
}

func TestHelperErase(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		preStore  bool
		wantErr   bool
	}{
		{
			name:      "erase existing credential",
			serverURL: "https://ghcr.io",
			preStore:  true,
		},
		{
			name:      "erase non-existent is no-op",
			serverURL: "https://unknown.io",
			preStore:  false,
		},
		{
			name:      "empty URL is no-op",
			serverURL: "",
			preStore:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := secrets.NewMockStore()
			helper := New(store)
			ctx := context.Background()

			if tt.preStore {
				cred := &Credential{ServerURL: tt.serverURL, Username: "user", Secret: "pass"}
				require.NoError(t, helper.Store(ctx, cred))
			}

			err := helper.Erase(ctx, tt.serverURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify credential is gone
			if tt.serverURL != "" {
				_, getErr := helper.Get(ctx, tt.serverURL)
				assert.Error(t, getErr)
			}
		})
	}
}

func TestHelperList(t *testing.T) {
	tests := []struct {
		name       string
		setupStore func(*secrets.MockStore)
		want       map[string]string
		wantErr    bool
	}{
		{
			name:       "empty store",
			setupStore: func(_ *secrets.MockStore) {},
			want:       map[string]string{},
		},
		{
			name: "multiple credentials",
			setupStore: func(m *secrets.MockStore) {
				for _, c := range []Credential{
					{ServerURL: "https://ghcr.io", Username: "user1", Secret: "t1"},
					{ServerURL: "https://docker.io", Username: "user2", Secret: "t2"},
				} {
					data, _ := json.Marshal(c)
					m.Data["credhelper:"+c.ServerURL] = data
				}
			},
			want: map[string]string{
				"https://ghcr.io":   "user1",
				"https://docker.io": "user2",
			},
		},
		{
			name: "ignores non-credhelper keys",
			setupStore: func(m *secrets.MockStore) {
				cred := Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "t1"}
				data, _ := json.Marshal(cred)
				m.Data["credhelper:https://ghcr.io"] = data
				m.Data["auth:github"] = []byte("something")
				m.Data["other-key"] = []byte("value")
			},
			want: map[string]string{"https://ghcr.io": "user1"},
		},
		{
			name: "list error propagated",
			setupStore: func(m *secrets.MockStore) {
				m.ListErr = assert.AnError
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := secrets.NewMockStore()
			tt.setupStore(store)

			helper := New(store)
			got, err := helper.List(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCredentialJSON(t *testing.T) {
	cred := Credential{
		ServerURL: "https://ghcr.io",
		Username:  "user1",
		Secret:    "token1",
	}
	data, err := json.Marshal(cred)
	require.NoError(t, err)

	// Verify Docker protocol field names (capital case)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "ServerURL")
	assert.Contains(t, raw, "Username")
	assert.Contains(t, raw, "Secret")
}

func TestErrorResponseJSON(t *testing.T) {
	resp := ErrorResponse{Message: "credentials not found"}
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Verify Docker protocol error format
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "message")
	assert.Equal(t, "credentials not found", raw["message"])
}

// Benchmarks

func BenchmarkHelperGet(b *testing.B) {
	store := secrets.NewMockStore()
	cred := Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "token1"}
	data, _ := json.Marshal(cred)
	store.Data["credhelper:https://ghcr.io"] = data

	helper := New(store)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = helper.Get(ctx, "https://ghcr.io")
	}
}

func BenchmarkHelperStore(b *testing.B) {
	store := secrets.NewMockStore()
	helper := New(store)
	ctx := context.Background()
	cred := &Credential{ServerURL: "https://ghcr.io", Username: "user1", Secret: "token1"}

	b.ResetTimer()
	for b.Loop() {
		_ = helper.Store(ctx, cred)
	}
}

func BenchmarkHelperErase(b *testing.B) {
	store := secrets.NewMockStore()
	helper := New(store)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_ = helper.Erase(ctx, "https://ghcr.io")
	}
}

func BenchmarkHelperList(b *testing.B) {
	store := secrets.NewMockStore()
	for i := range 10 {
		cred := Credential{ServerURL: "https://registry" + string(rune('0'+i)) + ".io", Username: "user"}
		data, _ := json.Marshal(cred)
		store.Data["credhelper:"+cred.ServerURL] = data
	}

	helper := New(store)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = helper.List(ctx)
	}
}
