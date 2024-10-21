package main

import (
	"fmt"
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

func Test_env_bool(t *testing.T) {
	tests := []struct {
		name   string
		envkey string
		envval string
		want   bool
	}{
		{
			name:   "Environment variable set to true",
			envkey: "TEST_ENV_BOOL",
			envval: "true",
			want:   true,
		},
		{
			name:   "Environment variable set to false",
			envkey: "TEST_ENV_BOOL",
			envval: "false",
			want:   false,
		},
		{
			name:   "Environment variable not set",
			envkey: "TEST_ENV_BOOL",
			envval: "",
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envval != "" {
				os.Setenv(tt.envkey, tt.envval)
			} else {
				os.Unsetenv(tt.envkey)
			}
			if got := env_bool(tt.envkey); got != tt.want {
				t.Errorf("env_bool() = %v, want %v", got, tt.want)
			}
		})
	}

	envkey := "TEST_ENV_BOOL"
	envVal := "invalid"
	// Test panic
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		} else {
			t.Logf("Recovered from panic: %v", r)
			if r.(error).Error() != fmt.Sprintf("unable to parse environment variable %s with value %s to bool", envkey, envVal) {
				t.Errorf("Unexpected panic message: %v", r)
			}
		}
	}()

	os.Setenv(envkey, envVal)
	env_bool("TEST_ENV_BOOL")
}

func TestMain(m *testing.M) {
	GenerateTestCertAndKey()
	code := m.Run() // run tests
	os.Exit(code)
}
