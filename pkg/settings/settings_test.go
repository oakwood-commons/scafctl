package settings

import (
	"testing"
)

func TestNewCliParams(t *testing.T) {
	tests := []struct {
		name string
		want *Run
	}{
		{
			name: "default CLI params",
			want: &Run{
				MinLogLevel: 0,
				EntryPointSettings: EntryPointSettings{
					FromAPI: false,
					FromCli: true,
					Path:    "",
				},
				IsQuiet:     false,
				NoColor:     false,
				ExitOnError: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewCliParams()
			if *got != *tt.want {
				t.Errorf("NewCliParams() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
