package state

import (
	"context"
	"fmt"
	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/core/mcpproxy"
	"mcp-gateway/internal/template"
	"mcp-gateway/pkg/mcp"
	"time"

	"github.com/ifuryst/lol"
	"go.uber.org/zap"
)

func (s *State) setRouter(prefix string, router *config.RouterConfig) {
	runtime := s.getRuntime(prefix)
	runtime.Router = router
	s.Runtime[uriPrefix(prefix)] = runtime
}

// BuildStateFromConfig creates a new State from the given configuration
func BuildStateFromConfig(ctx context.Context, cfgs []*config.MCPConfig, oldState *State, logger *zap.Logger) (*State, error) {
	// Create new state
	newState := NewState()
	newState.RawConfigs = cfgs

	for _, cfg := range cfgs {
		toolMap := make(map[toolName]*config.ToolConfig)
		// Initialize tool map and list for MCP servers
		for _, tool := range cfg.Tools {
			newState.Metrics.totalTools++
			toolMap[toolName(tool.Name)] = &tool
		}

		// Build prefix to tools mapping for MCP servers
		prefixMap := make(map[string][]string)
		// Support multiple prefixes for a single server
		for _, router := range cfg.Routers {
			prefixMap[router.Server] = append(prefixMap[router.Server], router.Prefix)
			newState.setRouter(router.Prefix, &router)
			logger.Info("registered router",
				zap.String("tenant", cfg.AppCode),
				zap.String("prefix", router.Prefix),
				zap.String("server", router.Server))
		}

		for k, v := range prefixMap {
			prefixMap[k] = lol.UniqSlice(v)
		}

		// Process regular HTTP servers
		for _, server := range cfg.Servers {
			newState.Metrics.httpServers++
			prefixes, ok := prefixMap[server.Name]
			if !ok {
				newState.Metrics.idleHTTPServers++
				logger.Warn("failed to find prefix for server", zap.String("server", server.Name))
				continue
			}

			var (
				allowedToolSchemas []mcp.ToolSchema
				allowedTools       = make(map[toolName]*config.ToolConfig)
			)
			for _, ss := range server.AllowedTools {
				tool, ok := toolMap[toolName(ss)]
				if ok {
					allowedToolSchemas = append(allowedToolSchemas, tool.ToToolSchema())
					allowedTools[toolName(ss)] = tool
				} else {
					newState.Metrics.missingTools++
					logger.Warn("failed to find allowed tool for server", zap.String("server", server.Name),
						zap.String("tool", ss))
				}
			}

			// Process each prefix for this server
			for _, prefix := range prefixes {
				runtime := newState.getRuntime(prefix)
				runtime.ProtoType = cnst.BackendProtoHttp
				runtime.Server = &server
				runtime.Tools = allowedTools
				runtime.ToolSchemas = allowedToolSchemas
				//runtime.prompts = map[promptName]*cfg.Prompts
				for i := range cfg.Prompts {
					p := &cfg.Prompts[i]
					runtime.Prompts[promptName(p.Name)] = p
				}
				//runtime.promptSchemas = cfg.Prompts.ToPromptSchemas()
				runtime.PromptSchemas = make([]mcp.PromptSchema, len(cfg.Prompts))
				for i, p := range cfg.Prompts {
					runtime.PromptSchemas[i] = p.ToPromptSchema()
				}
				newState.Runtime[uriPrefix(prefix)] = runtime
			}
		}

		// Process MCP servers
		for _, mcpServer := range cfg.McpServers {
			newState.Metrics.mcpServers++
			prefixes, exists := prefixMap[mcpServer.Name]
			if !exists {
				newState.Metrics.idleMCPServers++
				logger.Warn("failed to find prefix for mcp server", zap.String("server", mcpServer.Name))
				continue // Skip MCP servers without router prefix
			}

			// Process each prefix for this MCP server
			for _, prefix := range prefixes {
				runtime := newState.getRuntime(prefix)
				runtime.McpServer = &mcpServer

				// Check if we already have transport with the same configuration
				var transport mcpproxy.Transport
				if oldState != nil {
					if oldRuntime, exists := oldState.Runtime[uriPrefix(prefix)]; exists {
						// Compare configurations to see if we need to create a new transport
						oldConfig := oldRuntime.McpServer
						if oldConfig != nil && oldConfig.Type == mcpServer.Type &&
							oldConfig.Command == mcpServer.Command &&
							oldConfig.URL == mcpServer.URL &&
							len(oldConfig.Args) == len(mcpServer.Args) {
							// Compare args
							argsMatch := true
							for i, arg := range oldConfig.Args {
								if arg != mcpServer.Args[i] {
									argsMatch = false
									break
								}
							}
							if argsMatch {
								// Reuse existing transport
								transport = oldRuntime.Transport
							}
						}
					}
				}

				// Create new transport if needed
				if transport == nil {
					var err error
					transport, err = mcpproxy.NewTransport(mcpServer)
					if err != nil {
						return nil, fmt.Errorf("failed to create transport for server %s: %w", mcpServer.Name, err)
					}
				}

				// Handle server startup based on policy and preinstalled flag
				if mcpServer.Policy == cnst.PolicyOnStart {
					// If PolicyOnStart is set, just start the server and keep it running
					go startMCPServer(ctx, logger, prefix, mcpServer, transport, false)
				} else if mcpServer.Preinstalled {
					// If Preinstalled is set but not PolicyOnStart, verify installation by starting and stopping
					go startMCPServer(ctx, logger, prefix, mcpServer, transport, true)
				}
				runtime.Transport = transport

				// Map protocol type based on server type
				switch mcpServer.Type {
				case cnst.BackendProtoStdio.String():
					runtime.ProtoType = cnst.BackendProtoStdio
				case cnst.BackendProtoSSE.String():
					runtime.ProtoType = cnst.BackendProtoSSE
				case cnst.BackendProtoStreamable.String():
					runtime.ProtoType = cnst.BackendProtoStreamable
				}
				newState.Runtime[uriPrefix(prefix)] = runtime
			}
		}
	}

	if oldState != nil {
		for prefix, oldRuntime := range oldState.Runtime {
			if _, stillExists := newState.Runtime[prefix]; !stillExists {
				if oldRuntime.McpServer == nil {
					continue
				}
				if oldRuntime.Transport == nil {
					logger.Info("transport already stopped", zap.String("prefix", string(prefix)),
						zap.String("command", oldRuntime.McpServer.Command), zap.Strings("args", oldRuntime.McpServer.Args))
					continue
				}
				logger.Info("shutting down unused transport", zap.String("prefix", string(prefix)),
					zap.String("command", oldRuntime.McpServer.Command), zap.Strings("args", oldRuntime.McpServer.Args))
				if err := oldRuntime.Transport.Stop(ctx); err != nil {
					logger.Warn("failed to close old transport", zap.String("prefix", string(prefix)),
						zap.Error(err), zap.String("command", oldRuntime.McpServer.Command),
						zap.Strings("args", oldRuntime.McpServer.Args))
				}
			}
		}
	}

	return newState, nil
}

func (s *State) DeleteRuntimeByPrefixes(ctx context.Context, prefixes []string, logger *zap.Logger) {
	for _, prefix := range prefixes {
		if runtime, exists := s.Runtime[uriPrefix(prefix)]; exists {
			// If there's an MCP server running, stop it first
			if runtime.McpServer != nil && runtime.Transport != nil {
				logger.Info("stopping transport for prefix before deletion",
					zap.String("prefix", prefix),
					zap.String("command", runtime.McpServer.Command))

				if err := runtime.Transport.Stop(ctx); err != nil {
					logger.Warn("failed to stop transport while deleting prefix",
						zap.String("prefix", prefix),
						zap.Error(err))
				}
			}

			// Delete the runtime entry
			delete(s.Runtime, uriPrefix(prefix))
			logger.Info("deleted runtime configuration",
				zap.String("prefix", prefix))
		}
	}
}

func (s *State) GetRouteStateMap() map[uriPrefix]runtimeUnit {
	return s.Runtime
}

// UpdateConsumerToken updates a consumer token and returns true if any token was changed.
func (s *State) UpdateConsumerToken(oldToken, newToken string, prefixes []string) bool {
	changed := false
	for prefix, runtime := range s.Runtime {
		// If prefixes are specified, only update the specified prefixes
		if len(prefixes) > 0 {
			found := false
			for _, p := range prefixes {
				if p == string(prefix) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if runtime.Router != nil {
			for i, token := range runtime.Router.ConsumerTokens {
				if token == oldToken {
					runtime.Router.ConsumerTokens[i] = newToken
					changed = true
				}
			}
		}
	}
	return changed
}

func UpdateStateFromConfig(ctx context.Context, cfgs []*config.MCPConfig, oldState *State, logger *zap.Logger) (*State, error) {
	for _, cfg := range cfgs {
		toolMap := make(map[toolName]*config.ToolConfig)
		// Initialize tool map and list for MCP servers
		for _, tool := range cfg.Tools {
			oldState.Metrics.totalTools++
			toolMap[toolName(tool.Name)] = &tool
		}

		// Build prefix to tools mapping for MCP servers
		prefixMap := make(map[string][]string)
		// Support multiple prefixes for a single server
		for _, router := range cfg.Routers {
			prefixMap[router.Server] = append(prefixMap[router.Server], router.Prefix)
			oldState.setRouter(router.Prefix, &router)
			logger.Info("registered router",
				zap.String("tenant", cfg.AppCode),
				zap.String("prefix", router.Prefix),
				zap.String("server", router.Server))
		}

		for k, v := range prefixMap {
			prefixMap[k] = lol.UniqSlice(v)
		}

		// Process regular HTTP servers
		for _, server := range cfg.Servers {
			oldState.Metrics.httpServers++
			prefixes, ok := prefixMap[server.Name]
			if !ok {
				oldState.Metrics.idleHTTPServers++
				logger.Warn("failed to find prefix for server", zap.String("server", server.Name))
				continue
			}

			var (
				allowedToolSchemas []mcp.ToolSchema
				allowedTools       = make(map[toolName]*config.ToolConfig)
			)
			for _, ss := range server.AllowedTools {
				tool, ok := toolMap[toolName(ss)]
				if ok {
					allowedToolSchemas = append(allowedToolSchemas, tool.ToToolSchema())
					allowedTools[toolName(ss)] = tool
				} else {
					oldState.Metrics.missingTools++
					logger.Warn("failed to find allowed tool for server", zap.String("server", server.Name),
						zap.String("tool", ss))
				}
			}

			// Process each prefix for this server
			for _, prefix := range prefixes {
				runtime := oldState.getRuntime(prefix)
				runtime.ProtoType = cnst.BackendProtoHttp
				runtime.Server = &server
				runtime.Tools = allowedTools
				runtime.ToolSchemas = allowedToolSchemas
				//runtime.prompts = map[promptName]*cfg.Prompts
				for i := range cfg.Prompts {
					p := &cfg.Prompts[i]
					runtime.Prompts[promptName(p.Name)] = p
				}
				//runtime.promptSchemas = cfg.Prompts.ToPromptSchemas()
				runtime.PromptSchemas = make([]mcp.PromptSchema, len(cfg.Prompts))
				for i, p := range cfg.Prompts {
					runtime.PromptSchemas[i] = p.ToPromptSchema()
				}
				oldState.Runtime[uriPrefix(prefix)] = runtime
			}
		}

		// Process MCP servers
		for _, mcpServer := range cfg.McpServers {
			oldState.Metrics.mcpServers++
			prefixes, exists := prefixMap[mcpServer.Name]
			if !exists {
				oldState.Metrics.idleMCPServers++
				logger.Warn("failed to find prefix for mcp server", zap.String("server", mcpServer.Name))
				continue // Skip MCP servers without router prefix
			}

			// Process each prefix for this MCP server
			for _, prefix := range prefixes {
				runtime := oldState.getRuntime(prefix)
				runtime.McpServer = &mcpServer

				// Check if we already have transport with the same configuration
				var transport mcpproxy.Transport
				if oldState != nil {
					if oldRuntime, exists := oldState.Runtime[uriPrefix(prefix)]; exists {
						// Compare configurations to see if we need to create a new transport
						oldConfig := oldRuntime.McpServer
						if oldConfig != nil && oldConfig.Type == mcpServer.Type &&
							oldConfig.Command == mcpServer.Command &&
							oldConfig.URL == mcpServer.URL &&
							len(oldConfig.Args) == len(mcpServer.Args) {
							// Compare args
							argsMatch := true
							for i, arg := range oldConfig.Args {
								if arg != mcpServer.Args[i] {
									argsMatch = false
									break
								}
							}
							if argsMatch {
								// Reuse existing transport
								transport = oldRuntime.Transport
							}
						}
					}
				}

				// Create new transport if needed
				if transport == nil {
					var err error
					transport, err = mcpproxy.NewTransport(mcpServer)
					if err != nil {
						return nil, fmt.Errorf("failed to create transport for server %s: %w", mcpServer.Name, err)
					}
				}

				// Handle server startup based on policy and preinstalled flag
				if mcpServer.Policy == cnst.PolicyOnStart {
					// If PolicyOnStart is set, just start the server and keep it running
					go startMCPServer(ctx, logger, prefix, mcpServer, transport, false)
				} else if mcpServer.Preinstalled {
					// If Preinstalled is set but not PolicyOnStart, verify installation by starting and stopping
					go startMCPServer(ctx, logger, prefix, mcpServer, transport, true)
				}
				runtime.Transport = transport

				// Map protocol type based on server type
				switch mcpServer.Type {
				case cnst.BackendProtoStdio.String():
					runtime.ProtoType = cnst.BackendProtoStdio
				case cnst.BackendProtoSSE.String():
					runtime.ProtoType = cnst.BackendProtoSSE
				case cnst.BackendProtoStreamable.String():
					runtime.ProtoType = cnst.BackendProtoStreamable
				}
				oldState.Runtime[uriPrefix(prefix)] = runtime
			}
		}
	}

	return oldState, nil
}

func startMCPServer(ctx context.Context, logger *zap.Logger, prefix string, mcpServer config.MCPServerConfig,
	transport mcpproxy.Transport, needStop bool) {
	// If Preinstalled is set but not PolicyOnStart, verify installation by starting and stopping
	if transport.IsRunning() {
		logger.Info("server already started, don't need to preinstall",
			zap.String("prefix", prefix),
			zap.String("command", mcpServer.Command),
			zap.Strings("args", mcpServer.Args))
		return
	}

	// Create a new context to avoid being canceled when the main context is canceled during shutdown
	startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := transport.Start(startCtx, template.NewContext()); err != nil {
		logger.Error("failed to start server for preinstall",
			zap.String("prefix", prefix),
			zap.String("command", mcpServer.Command),
			zap.Strings("args", mcpServer.Args),
			zap.Error(err))
	} else {
		logger.Info("server started for preinstall",
			zap.String("prefix", prefix),
			zap.String("command", mcpServer.Command),
			zap.Strings("args", mcpServer.Args))

		if needStop {
			// Stop the server after successful start
			if err := transport.Stop(startCtx); err != nil {
				logger.Error("failed to stop server for preinstall",
					zap.String("prefix", prefix),
					zap.String("command", mcpServer.Command),
					zap.Strings("args", mcpServer.Args),
					zap.Error(err))
			} else {
				logger.Info("server stopped for preinstall",
					zap.String("prefix", prefix),
					zap.String("command", mcpServer.Command),
					zap.Strings("args", mcpServer.Args))
			}
		}
	}
}
