package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jacobmiller22/gossentials/omniconfig"
)

type AntigravityCliConfig struct {
	BrainPath string `json:"brainPath"`
}

type LogConfig struct {
	Level string `json:"level"`
	File  string `json:"file"`
}

type ClientConfig struct{}

type ServerConfig struct {
	FilePollPeriod time.Duration `json:"filePollPeriod"`

	PidFilePath string `json:"pidFilePath"`
}

type Config struct {
	ConfigPath           string
	Log                  LogConfig            `json:"log"`
	Client               ClientConfig         `json:"db"`
	Server               ServerConfig         `json:"server"`
	AntigravityCliConfig AntigravityCliConfig `json:"antigravityCli"`

	SocketPath    string `json:"socketPath"`
	RestartDaemon bool   `json:"restartDaemon"`
}

const DEFAULT_HIVEMIND_CONFIG_FILENAME string = "hivemind.json"
const DEFAULT_HIVEMIND_LOGFILE_FILENAME string = "hivemind.log"

var ErrMissingConfig error = errors.New("missing config")
var ErrTooFewArgs error = errors.New("too few args")
var ErrUnknownLogLevel error = errors.New("unknown log level")

func parseLogLevel(s string) (slog.Leveler, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelWarn, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "fatal", "error":
		return slog.LevelError, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownLogLevel, s)
	}
}

func parseLogFilepath(s string) (io.Writer, error) {
	if s == "stderr" {
		return os.Stderr, nil
	}
	if s == "stdout" {
		return os.Stdout, nil
	}

	if err := os.MkdirAll(filepath.Dir(s), 0644); err != nil {
		return nil, err
	}

	fd, err := os.OpenFile(s, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return fd, nil
}

func (c *Config) LogLeveler() slog.Leveler {
	leveler, err := parseLogLevel(c.Log.Level)
	if err != nil {
		return slog.LevelError
	}
	return leveler
}

func (c *Config) Logger() (*slog.Logger, error) {
	logwriter, err := parseLogFilepath(c.Log.File)
	if err != nil {
		return nil, err
	}

	logger := slog.New(slog.NewJSONHandler(
		logwriter,
		&slog.HandlerOptions{
			AddSource: c.LogLeveler() == slog.LevelDebug,
			Level:     c.LogLeveler(),
		},
	)).With("pid", os.Getpid())

	return logger, nil
}

var DefaultConfig *Config = &Config{
	ConfigPath: "~/.config/hivemind/config.json",
	Log: LogConfig{
		Level: "debug",
		File:  DefaultLogFilePath(),
	},
	Client: ClientConfig{},
	Server: ServerConfig{
		FilePollPeriod: 1 * time.Second,
		PidFilePath:    "~/.config/hivemind/hivemind.pid",
	},

	SocketPath:    "~/.config/hivemind/hivemind.sock",
	RestartDaemon: false,
}

func DefaultConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = path.Join(os.Getenv("HOME"), ".config")
	}
	return path.Join(base, DEFAULT_HIVEMIND_CONFIG_FILENAME)
}

func DefaultLogFilePath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = path.Join(os.Getenv("HOME"), ".local/share")
	}
	return path.Join(base, DEFAULT_HIVEMIND_LOGFILE_FILENAME)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	if err != nil {
		return false
	}
	return true
}

var defaultConfigurer omniconfig.StaticConfigurer[Config] = omniconfig.StaticConfigurer[Config]{
	Config: DefaultConfig,
}

type emptyConfigurer[T comparable] struct{}

func (e *emptyConfigurer[T]) Load() (*T, error) {
	return nil, nil
}

func newJsonFileConfigurerer(path string) omniconfig.Configurer[Config] {
	configurer, err := omniconfig.NewFsIOConfigurer[Config](path)

	if err != nil {
		return &emptyConfigurer[Config]{}
	}

	return configurer.With(func(i *omniconfig.IOConfigurer[Config]) {
		i.Processor = omniconfig.JsonReaderProcessor[Config]
	})
}

func newDefaultFlagConfigurer(args []string) *omniconfig.FlagConfigurer[Config] {
	flagset := flag.NewFlagSet(
		"hivemind",
		flag.ContinueOnError,
	)
	var cfg Config
	flagset.StringVar(&cfg.ConfigPath, "config", DefaultConfigPath(), "Path to configuration file. Uses default if not provided.")
	flagset.StringVar(&cfg.Log.Level, "log-level", "", "Log level to use")
	flagset.StringVar(&cfg.Log.File, "log-file", "", "Log file to use")
	flagset.StringVar(&cfg.SocketPath, "uds", "", "Custom path to the Unix Domain Socket")
	flagset.StringVar(&cfg.AntigravityCliConfig.BrainPath, "antigravity-dir", "", "Custom path to search for Antigravity transcript files")
	flagset.DurationVar(&cfg.Server.FilePollPeriod, "file-poll", 0, "Polling interval for file adapters")
	flagset.BoolVar(&cfg.RestartDaemon, "restart", false, "Restart the background daemon if it is already running")

	return omniconfig.NewFlagConfigurer(
		flagset,
		&cfg,
		omniconfig.WithFlagConfigurerArgs[Config](args),
	)
}

var ErrConfigExists error = errors.New("configuration file exists at path")

// InitDefaultConfig creates the default configuration file at the specified path
func InitDefaultConfig() error {
	path := DefaultConfigPath()

	if fileExists(path) {
		return fmt.Errorf("%w: %s", ErrConfigExists, path)
	}

	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(fd).Encode(DefaultConfig); err != nil {
		return err
	}

	return nil
}

func LoadConfig(args []string) *Config {
	flagConfigurer := newDefaultFlagConfigurer(args)

	cfg, err := flagConfigurer.Load()
	if err != nil || cfg == nil {
		fmt.Printf("Error parsing flag: %+v\n", err)

		return DefaultConfig
	}

	cfg, _, err = omniconfig.MergeConfigurers(
		defaultConfigurer,
		newJsonFileConfigurerer(cfg.ConfigPath),
		flagConfigurer,
	)

	if err != nil {
		return DefaultConfig
	}

	return cfg
}
