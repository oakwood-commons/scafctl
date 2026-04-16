package <% .pkg_name %>

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProviders(t *testing.T) {
	p := &Plugin{}
	providers, err := p.GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{ProviderName}, providers)
}

func TestGetProviderDescriptor(t *testing.T) {
	p := &Plugin{}

	t.Run("known provider", func(t *testing.T) {
		desc, err := p.GetProviderDescriptor(context.Background(), ProviderName)
		require.NoError(t, err)
		assert.Equal(t, ProviderName, desc.Name)
		assert.NotEmpty(t, desc.Description)
		assert.NotNil(t, desc.Schema)
		assert.NotEmpty(t, desc.Capabilities)
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, err := p.GetProviderDescriptor(context.Background(), "unknown")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown provider")
	})
}

func TestExecuteProvider(t *testing.T) {
	p := &Plugin{}

	tests := []struct {
		name    string
		input   map[string]any
		want    string
		wantErr bool
	}{
		{
			name:  "basic input",
			input: map[string]any{"value": "hello"},
			want:  "hello",
		},
		{
			name:  "empty input",
			input: map[string]any{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.ExecuteProvider(context.Background(), ProviderName, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.Data["result"])
		})
	}
}

func TestExecuteProvider_UnknownProvider(t *testing.T) {
	p := &Plugin{}
	_, err := p.ExecuteProvider(context.Background(), "unknown", nil)
	assert.Error(t, err)
}

func TestDescribeWhatIf(t *testing.T) {
	p := &Plugin{}

	t.Run("with value", func(t *testing.T) {
		desc, err := p.DescribeWhatIf(context.Background(), ProviderName, map[string]any{"value": "test"})
		require.NoError(t, err)
		assert.Contains(t, desc, "test")
	})

	t.Run("empty value", func(t *testing.T) {
		desc, err := p.DescribeWhatIf(context.Background(), ProviderName, map[string]any{})
		require.NoError(t, err)
		assert.NotEmpty(t, desc)
	})
}

func BenchmarkExecuteProvider(b *testing.B) {
	p := &Plugin{}
	input := map[string]any{"value": "bench"}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = p.ExecuteProvider(ctx, ProviderName, input)
	}
}
