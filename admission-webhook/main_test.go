package main

import (
	"os"
	"testing"
)

func Test_env_float(t *testing.T) {
	defaultFloat := float32(5.0)
	tests := []struct {
		name   string
		envkey string
		envval string
		want   float32
	}{
		{
			name:   "Environment variable set to valid float",
			envkey: "TEST_ENV_FLOAT",
			envval: "3.14",
			want:   3.14,
		},
		{
			name:   "Environment variable set to invalid float",
			envkey: "TEST_ENV_FLOAT",
			envval: "invalid",
			want:   float32(defaultFloat),
		},
		{
			name:   "Environment variable not set",
			envkey: "TEST_ENV_FLOAT",
			envval: "",
			want:   float32(defaultFloat),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envval != "" {
				os.Setenv(tt.envkey, tt.envval)
			} else {
				os.Unsetenv(tt.envkey)
			}
			if got := env_float(tt.envkey, defaultFloat); got != tt.want {
				t.Errorf("env_float() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_env_int(t *testing.T) {
	defaultInt := 5
	tests := []struct {
		name   string
		envkey string
		envval string
		want   int
	}{
		{
			name:   "Environment variable set to valid int",
			envkey: "TEST_ENV_INT",
			envval: "10",
			want:   10,
		},
		{
			name:   "Environment variable set to invalid int",
			envkey: "TEST_ENV_INT",
			envval: "invalid",
			want:   defaultInt,
		},
		{
			name:   "Environment variable not set",
			envkey: "TEST_ENV_INT",
			envval: "",
			want:   defaultInt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envval != "" {
				os.Setenv(tt.envkey, tt.envval)
			} else {
				os.Unsetenv(tt.envkey)
			}
			if got := env_int(tt.envkey, defaultInt); got != tt.want {
				t.Errorf("env_int() = %v, want %v", got, tt.want)
			}
		})
	}
}
