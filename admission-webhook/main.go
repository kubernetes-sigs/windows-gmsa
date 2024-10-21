package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
)

func main() {
	initLogrus()

	enableCertReload := flag.Bool("cert-reload", false, "enable certificate reload")
	flag.Parse()

	kubeClient, err := createKubeClient()
	if err != nil {
		panic(err)
	}

	randomHostname := env_bool("RANDOM_HOSTNAME")

	options := []WebhookOption{WithCertReload(*enableCertReload)}
	options = append(options, WithRandomHostname(randomHostname))

	webhook := newWebhookWithOptions(kubeClient, options...)

	tlsConfig := &tlsConfig{
		crtPath: env("TLS_CRT"),
		keyPath: env("TLS_KEY"),
	}

	port := env_int("HTTPS_PORT", 443)

	if err = webhook.start(port, tlsConfig, nil); err != nil {
		panic(err)
	}
}

var logLevels = map[string]logrus.Level{
	"panic": logrus.PanicLevel,
	"fatal": logrus.FatalLevel,
	"error": logrus.ErrorLevel,
	"warn":  logrus.WarnLevel,
	"info":  logrus.InfoLevel,
	"debug": logrus.DebugLevel,
	"trace": logrus.TraceLevel,
}

func initLogrus() {
	logrus.SetOutput(os.Stdout)

	logLevel := logrus.DebugLevel
	invalid := false

	rawLogLevel, present := os.LookupEnv("LOG_LEVEL")
	if present {
		if level, valid := logLevels[strings.ToLower(rawLogLevel)]; valid {
			logLevel = level
		} else {
			invalid = true
		}
	}

	logrus.SetLevel(logLevel)

	if invalid {
		keys := make([]string, len(logLevels))
		i := 0
		for key := range logLevels {
			keys[i] = key
			i++
		}
		logrus.Warningf("Unknown log level %s, valid log levels are: %v", rawLogLevel, strings.Join(keys, ", "))
	}
}

func createKubeClient() (*kubeClient, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	config.QPS = env_float("QPS", rest.DefaultQPS)
	config.Burst = env_int("BURST", rest.DefaultBurst)
	logrus.Infof("QPS: %f, Burst: %d", config.QPS, config.Burst)

	return newKubeClient(config)
}

func env_float(key string, defaultFloat float32) float32 {
	if v, found := os.LookupEnv(key); found {
		if i, err := strconv.ParseFloat(v, 32); err == nil {
			return float32(i)
		}
		logrus.Warningf("unable to parse environment variable %s with value %s; using default value %f", key, v, defaultFloat)
	}

	return defaultFloat
}

func env_bool(key string) bool {
	if v, found := os.LookupEnv(key); found {
		// Convert string to bool
		if boolValue, err := strconv.ParseBool(v); err == nil {
			return boolValue
		}
		// throw error if unable to parse
		panic(fmt.Errorf("unable to parse environment variable %s with value %s to bool", key, v))
	}

	// return bool default value: false
	return false
}

func env_int(key string, defaultInt int) int {
	if v, found := os.LookupEnv(key); found {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
		logrus.Warningf("unable to parse environment variable %s with value %s; using default value %d", key, v, defaultInt)
	}

	return defaultInt
}

func env(key string) string {
	if value, found := os.LookupEnv(key); found {
		return value
	}
	panic(fmt.Errorf("%s env var not found", key))
}
