package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
)

func main() {
	initLogrus()

	kubeClient, err := createKubeClient()
	if err != nil {
		panic(err)
	}

	webhook := newWebhook(kubeClient)

	tlsConfig := &tlsConfig{
		crtPath: env("TLS_CRT"),
		keyPath: env("TLS_KEY"),
	}

	port, err := port("HTTPS_PORT")
	if err != nil {
		panic(err)
	}

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

	return newKubeClient(config)
}

func env(key string) string {
	if value, found := os.LookupEnv(key); found {
		return value
	}
	panic(fmt.Errorf("%s env var not found", key))
}

func port(key string) (int, error) {
	if port, found := os.LookupEnv(key); found {
		return strconv.Atoi(port)
	}
	return 443, nil
}
