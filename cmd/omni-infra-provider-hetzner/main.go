// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package main is the root cmd of the provider script.
package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/apricote/hcloud-upload-image/hcloudimages/v2"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/infra"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.yaml.in/yaml/v4"

	"github.com/coolguy1771/hetzner-infra-provider/internal/pkg/config"
	"github.com/coolguy1771/hetzner-infra-provider/internal/pkg/provider"
	"github.com/coolguy1771/hetzner-infra-provider/internal/pkg/provider/meta"
	"github.com/coolguy1771/hetzner-infra-provider/internal/version"
)

//go:embed data/schema.json
var schema string

//go:embed data/icon.svg
var icon []byte

var cfg struct {
	omniAPIEndpoint     string
	serviceAccountKey   string
	providerName        string
	providerDescription string
	configFile          string
	insecureSkipVerify  bool
	concurrency         uint
}

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "provider",
	Short:        "Hetzner Cloud Omni infrastructure provider",
	Long:         `Connects to Omni as an infra provider and manages servers in Hetzner Cloud`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := newLogger()
		if err != nil {
			return err
		}

		if cfg.omniAPIEndpoint == "" {
			return fmt.Errorf("omni-api-endpoint flag is not set")
		}

		hcloudClient, err := newHCloudClient()
		if err != nil {
			return err
		}

		provisioner := provider.NewProvisioner(hcloudClient, hcloudimages.NewClient(hcloudClient))

		ip, err := infra.NewProvider(meta.ProviderID, provisioner, infra.ProviderConfig{
			Name:        cfg.providerName,
			Description: cfg.providerDescription,
			Icon:        base64.RawStdEncoding.EncodeToString(icon),
			Schema:      schema,
		})
		if err != nil {
			return fmt.Errorf("failed to create infra provider: %w", err)
		}

		logger.Info("starting infra provider")

		clientOptions := []client.Option{
			client.WithInsecureSkipTLSVerify(cfg.insecureSkipVerify),
		}

		if cfg.serviceAccountKey != "" {
			clientOptions = append(clientOptions, client.WithServiceAccount(cfg.serviceAccountKey))
		}

		return ip.Run(cmd.Context(), logger,
			infra.WithOmniEndpoint(cfg.omniAPIEndpoint),
			infra.WithClientOptions(clientOptions...),
			infra.WithEncodeRequestIDsIntoTokens(),
			infra.WithVersion(version.Tag),
			infra.WithConcurrency(cfg.concurrency),
		)
	},
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up temporary Hetzner Cloud resources left over from interrupted image builds",
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := newLogger()
		if err != nil {
			return err
		}

		hcloudClient, err := newHCloudClient()
		if err != nil {
			return err
		}

		logger.Info("cleaning up temporary resources")

		return hcloudimages.NewClient(hcloudClient).CleanupTempResources(cmd.Context())
	},
}

func newLogger() (*zap.Logger, error) {
	loggerConfig := zap.NewProductionConfig()

	logger, err := loggerConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return logger, nil
}

func newHCloudClient() (*hcloud.Client, error) {
	var hetznerConfig config.Config

	if cfg.configFile != "" {
		configFile, err := os.Open(cfg.configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read Hetzner config file %q: %w", cfg.configFile, err)
		}
		defer configFile.Close() //nolint:errcheck

		if err = yaml.NewDecoder(configFile).Decode(&hetznerConfig); err != nil {
			return nil, fmt.Errorf("failed to parse Hetzner config file %q: %w", cfg.configFile, err)
		}
	}

	token := hetznerConfig.Hetzner.Token
	if token == "" {
		token = os.Getenv("HCLOUD_TOKEN")
	}

	if token == "" {
		return nil, fmt.Errorf("hetzner cloud API token is not set: provide it via the config file or HCLOUD_TOKEN")
	}

	opts := []hcloud.ClientOption{hcloud.WithToken(token)}

	if hetznerConfig.Hetzner.Endpoint != "" {
		opts = append(opts, hcloud.WithEndpoint(hetznerConfig.Hetzner.Endpoint))
	}

	return hcloud.NewClient(opts...), nil
}

func main() {
	if err := app(); err != nil {
		os.Exit(1)
	}
}

func app() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()

	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.Flags().StringVar(&cfg.omniAPIEndpoint, "omni-api-endpoint", os.Getenv("OMNI_ENDPOINT"),
		"the endpoint of the Omni API, if not set, defaults to OMNI_ENDPOINT env var.")
	rootCmd.Flags().StringVar(&meta.ProviderID, "id", meta.ProviderID, "the id of the infra provider, it is used to match the resources with the infra provider label.")
	rootCmd.Flags().StringVar(&cfg.serviceAccountKey, "omni-service-account-key", os.Getenv("OMNI_SERVICE_ACCOUNT_KEY"), "Omni service account key, if not set, defaults to OMNI_SERVICE_ACCOUNT_KEY.")
	rootCmd.Flags().StringVar(&cfg.providerName, "provider-name", "Hetzner", "provider name as it appears in Omni")
	rootCmd.Flags().StringVar(&cfg.providerDescription, "provider-description", "Hetzner Cloud infrastructure provider", "Provider description as it appears in Omni")
	rootCmd.Flags().BoolVar(&cfg.insecureSkipVerify, "insecure-skip-verify", false, "ignores untrusted certs on Omni side")
	rootCmd.Flags().UintVar(&cfg.concurrency, "concurrency", 4, "maximum number of machine requests to provision concurrently")
	rootCmd.PersistentFlags().StringVar(&cfg.configFile, "config-file", "", "Hetzner provider config file (optional if HCLOUD_TOKEN is set)")

	rootCmd.AddCommand(cleanupCmd)
}
