package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/InazumaV/V2bX/common/loghook"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/InazumaV/V2bX/node"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	config string
	watch  bool
)

var serverCommand = cobra.Command{
	Use:   "server",
	Short: "Run V2bX server",
	Run:   serverHandle,
	Args:  cobra.NoArgs,
}

func init() {
	serverCommand.PersistentFlags().
		StringVarP(&config, "config", "c",
			"/etc/V2bX/config.json", "config file path")
	serverCommand.PersistentFlags().
		BoolVarP(&watch, "watch", "w",
			true, "watch file path change")
	command.AddCommand(&serverCommand)
}

func serverHandle(_ *cobra.Command, _ []string) {
	if err := runServer(); err != nil {
		log.WithField("err", err).Error("V2bX server exited with an error")
		// Exit non zero so systemd sees a failure. Returning normally used to
		// leave the unit inactive with Restart=on-failure doing nothing, so a
		// node that could not start looked like a clean shutdown.
		os.Exit(1)
	}
}

// setLogLevel applies the configured level. Xray spells two levels differently
// than logrus, and an unknown value used to leave the previous level in place
// without telling anyone.
func setLogLevel(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		log.SetLevel(log.InfoLevel)
	case "trace":
		log.SetLevel(log.TraceLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "warn", "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	case "fatal":
		log.SetLevel(log.FatalLevel)
	case "panic":
		log.SetLevel(log.PanicLevel)
	default:
		log.SetLevel(log.InfoLevel)
		log.WithField("level", level).Warn("Unknown log level, using info")
	}
}

func runServer() error {
	showVersion()
	c := conf.New()
	err := c.LoadFromPath(config)
	if err != nil {
		return fmt.Errorf("load config file failed: %w", err)
	}
	setLogLevel(c.LogConfig.Level)
	// Collapse identical entries: one misconfigured panel or route can emit the
	// same error on every connection or every pull cycle.
	loghook.Install(log.StandardLogger(), loghook.DefaultWindow, log.WarnLevel)
	if c.LogConfig.Output != "" {
		w := &lumberjack.Logger{
			Filename:   c.LogConfig.Output,
			MaxSize:    100,
			MaxBackups: 3,
			MaxAge:     28,
			Compress:   true,
		}
		log.SetOutput(w)
	}
	limiter.Init()
	log.Info("Start V2bX...")
	vc, err := vCore.NewCore(c.CoresConfig)
	if err != nil {
		return fmt.Errorf("new core failed: %w", err)
	}
	err = vc.Start()
	if err != nil {
		return fmt.Errorf("start core failed: %w", err)
	}
	defer vc.Close()
	log.Info("Core ", vc.Type(), " started")
	nodes := node.New()
	err = nodes.Start(c.NodeConfig, vc)
	if err != nil {
		return fmt.Errorf("run nodes failed: %w", err)
	}
	log.Info("Nodes started")
	xdns := os.Getenv("XRAY_DNS_PATH")
	sdns := os.Getenv("SING_DNS_PATH")
	if watch {
		err = c.Watch(config, xdns, sdns, func() {
			nodes.Close()
			err = vc.Close()
			if err != nil {
				log.WithField("err", err).Error("Restart node failed")
				return
			}
			vc, err = vCore.NewCore(c.CoresConfig)
			if err != nil {
				log.WithField("err", err).Error("New core failed")
				return
			}
			err = vc.Start()
			if err != nil {
				log.WithField("err", err).Error("Start core failed")
				return
			}
			log.Info("Core ", vc.Type(), " restarted")
			err = nodes.Start(c.NodeConfig, vc)
			if err != nil {
				log.WithField("err", err).Error("Run nodes failed")
				return
			}
			log.Info("Nodes restarted")
			runtime.GC()
		})
		if err != nil {
			return fmt.Errorf("start watch failed: %w", err)
		}
	}
	// clear memory
	runtime.GC()
	// wait exit signal
	{
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)
		<-osSignals
	}

	return nil
}
