package orasclient

import (
	"slices"
	"testing"
)

func TestCredentialsArgs(t *testing.T) {
	tests := []struct {
		name  string
		creds Credentials
		want  []string
	}{
		{"none", Credentials{}, nil},
		{"username only relies on prior oras login", Credentials{Username: "astrona-io"}, nil},
		{"password only", Credentials{Password: "secret"}, []string{"--password-stdin"}},
		{"username and password", Credentials{Username: "astrona-io", Password: "secret"}, []string{"--username", "astrona-io", "--password-stdin"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.creds.args(); !slices.Equal(got, tt.want) {
				t.Errorf("args() = %q, want %q", got, tt.want)
			}
		})
	}
}
