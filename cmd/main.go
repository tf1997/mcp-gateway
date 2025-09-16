package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	"mcp-gateway/pkg/rpc/client"
	RPCServer "mcp-gateway/pkg/rpc/server"
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

	installServiceCmd = &cobra.Command{
		Use:   "install",
		Short: "Install mcp-gateway as a systemd service",
		Run: func(cmd *cobra.Command, args []string) {
			installService()
		},
	}

	uninstallServiceCmd = &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall mcp-gateway systemd service",
		Run: func(cmd *cobra.Command, args []string) {
			uninstallService()
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "conf", "c", cnst.MCPGatewayYaml, "path to configuration file, like /etc/unla/mcp-gateway.yaml")
	rootCmd.PersistentFlags().StringVar(&pidFile, "pid", "", "path to PID file")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(reloadCmd)
	rootCmd.AddCommand(installServiceCmd)
	rootCmd.AddCommand(uninstallServiceCmd)
}

func installService() {
	currentExecutable, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to get current executable path: %v\n", err)
		os.Exit(1)
	}

	targetExecutablePath := "/usr/local/bin/mcp-gateway"

	// Create /usr/local/bin if it doesn't exist (though it usually does)
	if err := os.MkdirAll("/usr/local/bin", 0755); err != nil {
		fmt.Printf("Failed to create /usr/local/bin: %v\n", err)
		os.Exit(1)
	}

	// Move the current executable to the target path
	fmt.Printf("Moving executable from %s to %s...\n", currentExecutable, targetExecutablePath)
	if err := os.Rename(currentExecutable, targetExecutablePath); err != nil {
		fmt.Printf("Failed to move executable: %v\n", err)
		os.Exit(1)
	}

	// Create /etc/mcp-gateway if it doesn't exist
	if err := os.MkdirAll("/etc/mcp-gateway", 0755); err != nil {
		fmt.Printf("Failed to create /etc/mcp-gateway: %v\n", err)
		os.Exit(1)
	}

	serviceContent := `[Unit]
Description=MCP Gateway Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/usr/local/bin
ExecStart=/usr/local/bin/mcp-gateway -c /etc/mcp-gateway/mcp-gateway.yaml
Restart=on-failure
StandardOutput=journal
StandardError=journal
SyslogIdentifier=mcp-gateway

[Install]
WantedBy=multi-user.target
`
	serviceFilePath := "/etc/systemd/system/mcp-gateway.service"

	fmt.Printf("Installing systemd service to %s...\n", serviceFilePath)
	err = os.WriteFile(serviceFilePath, []byte(serviceContent), 0644)
	if err != nil {
		fmt.Printf("Failed to write service file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Reloading systemd daemon...")
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Enabling mcp-gateway service...")
	cmd = exec.Command("systemctl", "enable", "mcp-gateway") // Changed from := to =
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to enable service: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting mcp-gateway service...")
	cmd = exec.Command("systemctl", "start", "mcp-gateway")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to start service: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("mcp-gateway service installed and started successfully.")
}

func uninstallService() {
	serviceFilePath := "/etc/systemd/system/mcp-gateway.service"

	fmt.Println("Stopping mcp-gateway service...")
	cmd := exec.Command("systemctl", "stop", "mcp-gateway")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to stop service: %v\n", err)
		// Don't exit, try to disable and remove anyway
	}

	fmt.Println("Disabling mcp-gateway service...")
	cmd = exec.Command("systemctl", "disable", "mcp-gateway") // Changed from := to =
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to disable service: %v\n", err)
		// Don't exit, try to remove anyway
	}

	fmt.Printf("Removing service file %s...\n", serviceFilePath)
	if err := os.Remove(serviceFilePath); err != nil {
		fmt.Printf("Failed to remove service file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Reloading systemd daemon...")
	cmd = exec.Command("systemctl", "daemon-reload") // Changed from := to =
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("mcp-gateway service uninstalled successfully.")
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

	if cfg.ClusterManager != "" {
		logger.Info("Cluster manager configured", zap.String("cluster_manager", cfg.ClusterManager))
		go client.Heartbeat(logger, cfg, utils.GetLocalIP())
	}

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
	server, err := core.NewServer(logger, cfg.Port, store, sessionStore, a, &cfg.Session, &cfg.Kafka)
	if err != nil {
		logger.Fatal("Failed to create server", zap.Error(err))
	}

	err = server.RegisterRoutes(ctx)
	if err != nil {
		logger.Fatal("failed to register routes",
			zap.Error(err))
	}

	if cfg.RPCPort > 0 {
		logger.Info("gRPC server enabled, start to intializing...", zap.Int("rpc_port", cfg.RPCPort))
		go RPCServer.Start(cfg.RPCPort, logger, server)
	}

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
