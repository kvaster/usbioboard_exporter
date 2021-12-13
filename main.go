package main

import (
	"context"
	"flag"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"usbioboard_exporter/ioboard"
	"usbioboard_exporter/log"
	"usbioboard_exporter/utils"
)

var configFile = flag.String("config", "/etc/lls-exporter/lls.yml", "config file")

type Config struct {
	Listen  string
	Devices []*ioboard.Config
}

func (s *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	s.Listen = ":8080"
	type plain Config
	return unmarshal((*plain)(s))
}

func main() {
	flag.Parse()
	log.ParseFlags()

	data, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.WithField("error", err).Fatal("error reading config file")
	}

	var config Config
	err = yaml.UnmarshalStrict(data, &config)
	if err != nil {
		log.WithField("error", err).Fatal("error parsing config file")
	}

	var exporters []*ioboard.Exporter
	for _, c := range config.Devices {
		exporters = append(exporters, ioboard.New(c))
	}

	log.Info("starting")

	result := utils.NewActionResult()

	for _, e := range exporters {
		_e := e
		go func() {
			err := _e.Run()
			if err != nil {
				result.Error(err)
			}
		}()
	}

	var serveMux http.ServeMux
	serveMux.Handle("/metrics", promhttp.Handler())
	httpServer := http.Server{Addr: config.Listen, Handler: &serveMux}
	go func() {
		log.Info("http serve started")
		defer log.Info("http serve stopped")

		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			result.Error(err)
		}
	}()

	go func() {
		stopChan := make(chan os.Signal)
		signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		<-stopChan

		result.Success()
	}()

	err = result.Result()
	if err != nil {
		log.WithError(err).Error("error while running")
	}

	timeout, cancel := context.WithTimeout(context.Background(), time.Second*30)
	_ = httpServer.Shutdown(timeout)
	cancel()

	for _, e := range exporters {
		e.Stop()
	}

	log.Info("stopped")
}
