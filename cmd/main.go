package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcp-gateway/internal/auth"
	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/core"
	"mcp-gateway/internal/mcp/session"
	"mcp-gateway/internal/mcp/storage"
	pidHelper "mcp-gateway/pkg/helper"
	"mcp-gateway/pkg/logger"
	"mcp-gateway/pkg/utils"
	"mcp-gateway/pkg/version"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	configPath string
	pidFile    string

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number of mcp-gateway",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("mcp-gateway version %s\n", version.Get())
		},
	}
	reloadCmd = &cobra.Command{
		Use:   "reload",
		Short: "Reload the configuration of a running mcp-gateway instance",
		Run: func(cmd *cobra.Command, args []string) {
			// Load config to get PID path if not provided via command line
			cfg, _, err := config.LoadConfig[config.MCPGatewayConfig](configPath)
			if err != nil {
				fmt.Printf("Failed to load config: %v\n", err)
				os.Exit(1)
			}

			// Use PID from config if not provided via command line
			if pidFile == "" {
				pidFile = cfg.PID
			}

			if err := utils.SendSignalToPIDFile(pidHelper.GetPIDPath(pidFile), syscall.SIGHUP); err != nil {
				fmt.Printf("Failed to send reload signal: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Reload signal sent successfully")
		},
	}
	
	rootCmd = &cobra.Command{
		Use:   cnst.CommandName,
		Short: "MCP Gateway service",
		Long:  `MCP Gateway is a service that provides API gateway functionality for MCP ecosystem`,
		Run: func(cmd *cobra.Command, args []string) {
			run()
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "conf", "c", cnst.MCPGatewayYaml, "path to configuration file, like /etc/unla/mcp-gateway.yaml")
	rootCmd.PersistentFlags().StringVar(&pidFile, "pid", "", "path to PID file")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(reloadCmd)
}

func run() {
	ctx, cancel := context.WithCancel(context.Background())

	// Load configuration first
	cfg, cfgPath, err := config.LoadConfig[config.MCPGatewayConfig](configPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to load service configuration: %v", err))
	}

	// Initialize logger with configuration
	logger, err := logger.NewLogger(&cfg.Logger)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger.Info("Loaded configuration", zap.String("path", cfgPath))

	logger.Info("Starting mcp-gateway", zap.String("version", version.Get()))

	//Initialize storage and load initial configuration
	store, err := storage.NewStore(logger, &cfg.Storage)
	if err != nil {
		logger.Fatal("failed to initialize storage",
			zap.String("type", cfg.Storage.Type),
			zap.Error(err))
	}

	// Initialize session store
	// Note: server.kafkaProducer and server.nodeIP are not available here yet.
	// The session store will be re-initialized in core.NewServer with these values.
	sessionStore, err := session.NewStore(logger, nil, &cfg.Session, "") // Pass nil for kafkaProducer and empty string for nodeIP initially
	if err != nil {
		logger.Fatal("failed to initialize session store",
			zap.String("type", cfg.Session.Type),
			zap.Error(err))
	}

	// Initialize auth service
	a, err := auth.NewAuth(logger, cfg.Auth)
	if err != nil {
		logger.Fatal("Failed to initialize auth service", zap.Error(err))
	}

	// Create server instance
	server, err := core.NewServer(logger, cfg.Port, store, sessionStore, a, &cfg.MCP, &cfg.Session)
	if err != nil {
		logger.Fatal("Failed to create server", zap.Error(err))
	}

	err = server.RegisterRoutes(ctx)
	if err != nil {
		logger.Fatal("failed to register routes",
			zap.Error(err))
	}

	// ntf, err := notifier.NewNotifier(ctx, logger, &cfg.Notifier)
	// if err != nil {
	// 	logger.Fatal("failed to initialize notifier",
	// 		zap.Error(err))
	// }
	// updateCh, err := ntf.Watch(ctx)
	// if err != nil {
	// 	logger.Fatal("failed to start watching for updates",
	// 		zap.Error(err))
	// }

	server.Start()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	//periodically save the configuration as a fallback mechanism for the notifier
	ticker := time.NewTicker(cfg.ReloadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-quit:
			logger.Info("Received shutdown signal, starting graceful shutdown")

			// First cancel the main context to stop accepting new requests
			cancel()

			// Shutdown the MCP server to close all SSE connections
			err = server.Shutdown(ctx)
			if err != nil {
				logger.Error("failed to shutdown MCP server",
					zap.Error(err))
			} else {
				logger.Info("MCP server shutdown completed successfully")
			}

			return
		case <-ticker.C:
			logger.Info("Received ticker signal", zap.Bool("reload_switch", cfg.ReloadSwitch))
			if cfg.ReloadSwitch {
				server.SaveState(ctx)
			}
		}

	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
