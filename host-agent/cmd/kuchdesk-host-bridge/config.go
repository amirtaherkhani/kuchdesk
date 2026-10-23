package main

import (
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type bridgeConfig struct {
	Listen             string `yaml:"listen"`
	TLSCert            string `yaml:"tlsCert"`
	TLSKey             string `yaml:"tlsKey"`
	Token              string `yaml:"token"`
	ProjectRoot        string `yaml:"projectRoot"`
	ForwardingRecovery bool   `yaml:"forwardingRecovery"`
}

func loadConfig(args []string) (bridgeConfig, error) {
	cfg := bridgeConfig{Listen: "127.0.0.1:8181"}
	flags := flag.NewFlagSet("kuchdesk-host-bridge", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	values := flags.String("values", "", "private YAML configuration file")
	flags.StringVar(&cfg.Listen, "listen", cfg.Listen, "explicit host TCP listen address")
	flags.StringVar(&cfg.TLSCert, "tls-cert", "", "TLS certificate file")
	flags.StringVar(&cfg.TLSKey, "tls-key", "", "TLS private key file")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return bridgeConfig{}, errors.New("invalid host bridge arguments")
	}
	if *values != "" {
		mixed := false
		flags.Visit(func(option *flag.Flag) { mixed = mixed || option.Name != "values" })
		if mixed {
			return bridgeConfig{}, errors.New("values cannot be combined with listener or TLS flags")
		}
		file, err := os.Open(*values)
		if err != nil {
			return bridgeConfig{}, errors.New("host bridge values unavailable")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 64<<10 {
			return bridgeConfig{}, errors.New("host bridge values must be a regular mode-0600 file under 64 KiB")
		}
		decoder := yaml.NewDecoder(io.LimitReader(file, 64<<10))
		decoder.KnownFields(true)
		cfg = bridgeConfig{}
		if decoder.Decode(&cfg) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
			return bridgeConfig{}, errors.New("invalid host bridge values")
		}
		for _, path := range []*string{&cfg.TLSCert, &cfg.TLSKey} {
			if *path != "" && !filepath.IsAbs(*path) {
				*path = filepath.Join(filepath.Dir(*values), *path)
			}
		}
	} else {
		if os.Getenv("KUCHDESK_HOST_BRIDGE_ENABLED") != "true" {
			return bridgeConfig{}, errors.New("use private -values YAML or set KUCHDESK_HOST_BRIDGE_ENABLED=true")
		}
		cfg.Token = os.Getenv("KUCHDESK_HOST_BRIDGE_TOKEN")
	}
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.TLSCert == "" || cfg.TLSKey == "" || len(cfg.Token) < 32 {
		return bridgeConfig{}, errors.New("TLS certificate, private key and a private bearer of at least 32 characters are required")
	}
	if cfg.ProjectRoot != "" && !filepath.IsAbs(cfg.ProjectRoot) {
		return bridgeConfig{}, errors.New("projectRoot must be absolute")
	}
	if cfg.ForwardingRecovery && cfg.ProjectRoot == "" {
		return bridgeConfig{}, errors.New("forwardingRecovery requires projectRoot")
	}
	if host, port, err := net.SplitHostPort(cfg.Listen); err != nil || host == "" || port == "" {
		return bridgeConfig{}, errors.New("listen must be an explicit host:port")
	}
	return cfg, nil
}
