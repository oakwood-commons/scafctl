package settings

import (
	"context"
	"testing"
)

func TestIntoContext(t *testing.T) {
	tests := []struct {
		name     string
		settings *Run
	}{
		// {
		// 	name:     "nil_settings",
		// 	settings: nil,
		// },
		{
			name:     "empty_settings",
			settings: &Run{},
		},
		{
			name: "settings_with_values",
			settings: &Run{
				NoColor:     true,
				ExitOnError: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx := IntoContext(ctx, tt.settings)

			if newCtx == nil {
				t.Fatal("IntoContext() returned nil context")
			}

			// Verify the context is different from the original
			if ctx == newCtx && tt.settings != nil {
				t.Error("IntoContext() should return a new context")
			}

			// Verify we can retrieve the value
			val := newCtx.Value(settingsContextKey)
			if tt.settings == nil && val != nil {
				t.Errorf("IntoContext() stored value = %v; want nil", val)
			}
			if tt.settings != nil {
				retrieved, ok := val.(*Run)
				if !ok {
					t.Fatal("IntoContext() stored value is not *Run")
				}
				if retrieved != tt.settings {
					t.Errorf("IntoContext() stored different settings pointer")
				}
			}
		})
	}
}

func TestFromContext(t *testing.T) {
	tests := []struct {
		name       string
		setupCtx   func() context.Context
		wantOk     bool
		wantNil    bool
		wantValues *Run
	}{
		{
			name: "context_with_settings",
			setupCtx: func() context.Context {
				settings := &Run{
					NoColor:     true,
					ExitOnError: false,
				}
				return IntoContext(context.Background(), settings)
			},
			wantOk:  true,
			wantNil: false,
			wantValues: &Run{
				NoColor:     true,
				ExitOnError: false,
			},
		},
		{
			name: "context_without_settings",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantOk:  false,
			wantNil: true,
		},
		// {
		// 	name: "context_with_nil_settings",
		// 	setupCtx: func() context.Context {
		// 		return IntoContext(context.Background(), nil)
		// 	},
		// 	wantOk:  false,
		// 	wantNil: true,
		// },
		{
			name: "context_with_wrong_type",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), settingsContextKey, "wrong type")
			},
			wantOk:  false,
			wantNil: true,
		},
		{
			name: "empty_settings_struct",
			setupCtx: func() context.Context {
				return IntoContext(context.Background(), &Run{})
			},
			wantOk:     true,
			wantNil:    false,
			wantValues: &Run{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			got, ok := FromContext(ctx)

			if ok != tt.wantOk {
				t.Errorf("FromContext() ok = %v; want %v", ok, tt.wantOk)
			}

			if tt.wantNil && got != nil {
				t.Errorf("FromContext() got = %v; want nil", got)
			}

			if !tt.wantNil && got == nil {
				t.Fatal("FromContext() returned nil; want non-nil")
			}

			if tt.wantValues != nil && got != nil {
				if got.NoColor != tt.wantValues.NoColor {
					t.Errorf("FromContext() NoColor = %v; want %v", got.NoColor, tt.wantValues.NoColor)
				}
				if got.ExitOnError != tt.wantValues.ExitOnError {
					t.Errorf("FromContext() ExitOnError = %v; want %v", got.ExitOnError, tt.wantValues.ExitOnError)
				}
			}
		})
	}
}

func TestIntoContext_FromContext_roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		settings *Run
	}{
		{
			name: "roundtrip_with_values",
			settings: &Run{
				NoColor:     true,
				ExitOnError: true,
			},
		},
		{
			name:     "roundtrip_empty_struct",
			settings: &Run{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Store settings in context
			ctxWithSettings := IntoContext(ctx, tt.settings)

			// Retrieve settings from context
			retrieved, ok := FromContext(ctxWithSettings)

			if !ok {
				t.Fatal("FromContext() failed to retrieve settings")
			}

			if retrieved != tt.settings {
				t.Error("FromContext() returned different settings pointer than stored")
			}

			if retrieved.NoColor != tt.settings.NoColor {
				t.Errorf("NoColor = %v; want %v", retrieved.NoColor, tt.settings.NoColor)
			}

			if retrieved.ExitOnError != tt.settings.ExitOnError {
				t.Errorf("ExitOnError = %v; want %v", retrieved.ExitOnError, tt.settings.ExitOnError)
			}
		})
	}
}
