package main

import (
	"errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/chihaya/chihaya/frontend/http"
	"github.com/chihaya/chihaya/frontend/udp"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/pkg/log"
	"github.com/chihaya/chihaya/pkg/metrics"
	"github.com/chihaya/chihaya/pkg/stop"
	"github.com/chihaya/chihaya/storage"
)

var e2eCmd *cobra.Command

// Run represents the state of a running instance of Chihaya.
type Run struct {
	configFilePath string
	storage        storage.Storage
	logic          *middleware.Logic
	sg             *stop.Group
}

// NewRun runs an instance of Chihaya.
func NewRun(configFilePath string) (*Run, error) {
	r := &Run{
		configFilePath: configFilePath,
	}

	return r, r.Start(nil)
}

// Start begins an instance of Chihaya.
// It is optional to provide an instance of the peer store to avoid the
// creation of a new one.
func (r *Run) Start(ps storage.Storage) error {
	configFile, err := ParseConfigFile(r.configFilePath)
	if err != nil {
		return errors.New("failed to read config: " + err.Error())
	}
	cfg := configFile.Chihaya

	r.sg = stop.NewGroup()

	if len(cfg.MetricsAddr) > 0 {
		log.Info("starting metrics server", log.Fields{"addr": cfg.MetricsAddr})
		r.sg.Add(metrics.NewServer(cfg.MetricsAddr))
	} else {
		log.Info("metrics disabled because of empty address")
	}

	if ps == nil {
		log.Info("starting storage", log.Fields{"name": cfg.Storage.Name})
		ps, err = storage.NewStorage(cfg.Storage.Name, cfg.Storage.Config)
		if err != nil {
			return errors.New("failed to create storage: " + err.Error())
		}
		log.Info("started storage", ps)
	}
	r.storage = ps

	preHooks, err := middleware.HooksFromHookConfigs(cfg.PreHooks, r.storage)
	if err != nil {
		return errors.New("failed to validate hook config: " + err.Error())
	}
	postHooks, err := middleware.HooksFromHookConfigs(cfg.PostHooks, r.storage)
	if err != nil {
		return errors.New("failed to validate hook config: " + err.Error())
	}

	log.Info("starting tracker logic", log.Fields{
		"prehooks":  cfg.PreHookNames(),
		"posthooks": cfg.PostHookNames(),
	})
	r.logic = middleware.NewLogic(cfg.ResponseConfig, r.storage, preHooks, postHooks)

	if cfg.HTTPConfig.Addr != "" {
		log.Info("starting HTTP frontend", cfg.HTTPConfig)
		httpfe, err := http.NewFrontend(r.logic, cfg.HTTPConfig)
		if err != nil {
			return err
		}
		r.sg.Add(httpfe)
	}

	if cfg.UDPConfig.Addr != "" {
		log.Info("starting UDP frontend", cfg.UDPConfig)
		udpfe, err := udp.NewFrontend(r.logic, cfg.UDPConfig)
		if err != nil {
			return err
		}
		r.sg.Add(udpfe)
	}

	return nil
}

func combineErrors(prefix string, errs []error) error {
	var errStrs []string
	for _, err := range errs {
		errStrs = append(errStrs, err.Error())
	}

	return errors.New(prefix + ": " + strings.Join(errStrs, "; "))
}

// Stop shuts down an instance of Chihaya.
func (r *Run) Stop(keepPeerStore bool) (storage.Storage, error) {
	log.Debug("stopping frontends and metrics server")
	if errs := r.sg.Stop().Wait(); len(errs) != 0 {
		return nil, combineErrors("failed while shutting down frontends", errs)
	}

	log.Debug("stopping logic")
	if errs := r.logic.Stop().Wait(); len(errs) != 0 {
		return nil, combineErrors("failed while shutting down middleware", errs)
	}

	if !keepPeerStore {
		log.Debug("stopping peer store")
		if errs := r.storage.Stop().Wait(); len(errs) != 0 {
			return nil, combineErrors("failed while shutting down peer store", errs)
		}
		r.storage = nil
	}

	return r.storage, nil
}

// RootRunCmdFunc implements a Cobra command that runs an instance of Chihaya
// and handles reloading and shutdown via process signals.
func RootRunCmdFunc(cmd *cobra.Command, _ []string) error {
	configFilePath, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	r, err := NewRun(configFilePath)
	if err != nil {
		return err
	}

	quit := make(chan os.Signal)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	reload := makeReloadChan()

	for {
		select {
		case <-reload:
			log.Info("reloading; received SIGUSR1")
			peerStore, err := r.Stop(true)
			if err != nil {
				return err
			}

			if err := r.Start(peerStore); err != nil {
				return err
			}
		case <-quit:
			log.Info("shutting down; received SIGINT/SIGTERM")
			if _, err := r.Stop(false); err != nil {
				return err
			}

			return nil
		}
	}
}

// RootPreRunCmdFunc handles command line flags for the Run command.
func RootPreRunCmdFunc(cmd *cobra.Command, _ []string) error {
	noColors, err := cmd.Flags().GetBool("nocolors")
	if err != nil {
		return err
	}
	if noColors {
		log.SetFormatter(&logrus.TextFormatter{DisableColors: true})
	}

	jsonLog, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonLog {
		log.SetFormatter(&logrus.JSONFormatter{})
		log.Info("enabled JSON logging")
	}

	debugLog, err := cmd.Flags().GetBool("debug")
	if err != nil {
		return err
	}
	if debugLog {
		log.SetDebug(true)
		log.Info("enabled debug logging")
	}

	return nil
}

// RootPostRunCmdFunc handles clean up of any state initialized by command line
// flags.
func RootPostRunCmdFunc(cmd *cobra.Command, args []string) error {
	return nil
}

func main() {
	var rootCmd = &cobra.Command{
		Use:                "chihaya",
		Short:              "BitTorrent Tracker",
		Long:               "A customizable, multi-protocol BitTorrent Tracker",
		PersistentPreRunE:  RootPreRunCmdFunc,
		RunE:               RootRunCmdFunc,
		PersistentPostRunE: RootPostRunCmdFunc,
	}

	rootCmd.PersistentFlags().Bool("debug", false, "enable debug logging")
	rootCmd.PersistentFlags().Bool("json", false, "enable json logging")
	if runtime.GOOS == "windows" {
		rootCmd.PersistentFlags().Bool("nocolors", true, "disable log coloring")
	} else {
		rootCmd.PersistentFlags().Bool("nocolors", false, "disable log coloring")
	}

	rootCmd.Flags().String("config", "/etc/chihaya.yaml", "location of configuration file")

	if e2eCmd != nil {
		rootCmd.AddCommand(e2eCmd)
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal("failed when executing root cobra command: " + err.Error())
	}
}
