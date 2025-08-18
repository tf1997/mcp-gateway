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

// shallowCopyRuntime creates a shallow copy of the runtime map.
func shallowCopyRuntime(original map[uriPrefix]runtimeUnit) map[uriPrefix]runtimeUnit {
	newMap := make(map[uriPrefix]runtimeUnit, len(original))
	for k, v := range original {
		newMap[k] = v
	}
	return newMap
}

// deepCopyRuntimeUnit creates a deep copy of a single runtime unit.
func deepCopyRuntimeUnit(original runtimeUnit) runtimeUnit {
	// Start with a shallow copy of the unit
	newUnit := original

	// Deep copy pointer fields by creating new structs and copying values
	if original.Router != nil {
		newRouter := *original.Router
		// Also deep copy slices within the struct
		if newRouter.ConsumerTokens != nil {
			newRouter.ConsumerTokens = append([]string(nil), newRouter.ConsumerTokens...)
		}
		newUnit.Router = &newRouter
	}
	if original.Server != nil {
		newServer := *original.Server
		newUnit.Server = &newServer
	}
	if original.McpServer != nil {
		newMcpServer := *original.McpServer
		newUnit.McpServer = &newMcpServer
	}

	// Shallow copy the transport interface, as it represents a shared resource.
	newUnit.Transport = original.Transport

	// Deep copy maps of pointers.
	if original.Tools != nil {
		newTools := make(map[toolName]*config.ToolConfig, len(original.Tools))
		for tk, tv := range original.Tools {
			newTools[tk] = tv
		}
		newUnit.Tools = newTools
	}
	if original.Prompts != nil {
		newPrompts := make(map[promptName]*config.PromptConfig, len(original.Prompts))
		for pk, pv := range original.Prompts {
			newPrompts[pk] = pv
		}
		newUnit.Prompts = newPrompts
	}

	// Deep copy slices.
	if original.ToolSchemas != nil {
		newUnit.ToolSchemas = append([]mcp.ToolSchema(nil), original.ToolSchemas...)
	}
	if original.PromptSchemas != nil {
		newUnit.PromptSchemas = append([]mcp.PromptSchema(nil), original.PromptSchemas...)
	}
	if original.ConsumerTokens != nil {
		newUnit.ConsumerTokens = append([]string(nil), original.ConsumerTokens...)
	}

	return newUnit
}

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
			newState.Metrics.TotalTools++
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
			newState.Metrics.HttpServers++
			prefixes, ok := prefixMap[server.Name]
			if !ok {
				newState.Metrics.IdleHTTPServers++
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
					newState.Metrics.MissingTools++
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
			newState.Metrics.McpServers++
			prefixes, exists := prefixMap[mcpServer.Name]
			if !exists {
				newState.Metrics.IdleMCPServers++
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Start with a shallow copy for copy-on-write.
	newRuntime := shallowCopyRuntime(s.Runtime)

	for _, prefix := range prefixes {
		if runtime, exists := newRuntime[uriPrefix(prefix)]; exists {
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

			// Delete the runtime entry from the new map.
			delete(newRuntime, uriPrefix(prefix))
			logger.Info("deleted runtime configuration",
				zap.String("prefix", prefix))
		}
	}

	s.Runtime = newRuntime
}

func (s *State) GetRouteStateMap() map[uriPrefix]runtimeUnit {
	return s.Runtime
}

// UpdateConsumerToken updates a consumer token and returns true if any token was changed.
func (s *State) UpdateConsumerToken(oldToken, newToken string, prefixes []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Start with a shallow copy for copy-on-write.
	newRuntime := shallowCopyRuntime(s.Runtime)
	changed := false
	copiedUnits := make(map[uriPrefix]bool)

	for prefix, runtime := range newRuntime {
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
					// This unit is being modified, so deep copy it if we haven't already.
					if !copiedUnits[prefix] {
						runtime = deepCopyRuntimeUnit(runtime)
						newRuntime[prefix] = runtime
						copiedUnits[prefix] = true
					}
					runtime.Router.ConsumerTokens[i] = newToken
					changed = true
				}
			}
		}
	}

	if changed {
		s.Runtime = newRuntime
	}

	return changed
}

func UpdateStateFromConfig(ctx context.Context, cfgs []*config.MCPConfig, oldState *State, logger *zap.Logger) (*State, error) {
	oldState.mu.Lock()
	defer oldState.mu.Unlock()

	// Start with a shallow copy of the runtime map for modification.
	newRuntime := shallowCopyRuntime(oldState.Runtime)

	for _, cfg := range cfgs {
		toolMap := make(map[toolName]*config.ToolConfig)
		for _, tool := range cfg.Tools {
			toolMap[toolName(tool.Name)] = &tool
		}

		prefixMap := make(map[string][]string)
		for _, router := range cfg.Routers {
			prefix := uriPrefix(router.Prefix)
			prefixMap[router.Server] = append(prefixMap[router.Server], router.Prefix)

			unit, exists := newRuntime[prefix]
			if exists {
				unit = deepCopyRuntimeUnit(unit)
			} else {
				unit = runtimeUnit{
					Tools:   make(map[toolName]*config.ToolConfig),
					Prompts: make(map[promptName]*config.PromptConfig),
				}
			}

			unit.Router = &router
			newRuntime[prefix] = unit

			logger.Info("registered router",
				zap.String("tenant", cfg.AppCode),
				zap.String("prefix", router.Prefix),
				zap.String("server", router.Server))
		}

		for k, v := range prefixMap {
			prefixMap[k] = lol.UniqSlice(v)
		}

		for _, server := range cfg.Servers {
			prefixes, ok := prefixMap[server.Name]
			if !ok {
				continue
			}

			var (
				allowedToolSchemas []mcp.ToolSchema
				allowedTools       = make(map[toolName]*config.ToolConfig)
			)
			for _, ss := range server.AllowedTools {
				if tool, ok := toolMap[toolName(ss)]; ok {
					allowedToolSchemas = append(allowedToolSchemas, tool.ToToolSchema())
					allowedTools[toolName(ss)] = tool
				}
			}

			for _, prefixStr := range prefixes {
				prefix := uriPrefix(prefixStr)
				runtime := newRuntime[prefix]
				runtime.ProtoType = cnst.BackendProtoHttp
				runtime.Server = &server
				runtime.Tools = allowedTools
				runtime.ToolSchemas = allowedToolSchemas
				for i := range cfg.Prompts {
					p := &cfg.Prompts[i]
					runtime.Prompts[promptName(p.Name)] = p
				}
				runtime.PromptSchemas = make([]mcp.PromptSchema, len(cfg.Prompts))
				for i, p := range cfg.Prompts {
					runtime.PromptSchemas[i] = p.ToPromptSchema()
				}
				newRuntime[prefix] = runtime
			}
		}

		for _, mcpServer := range cfg.McpServers {
			prefixes, exists := prefixMap[mcpServer.Name]
			if !exists {
				continue
			}

			for _, prefixStr := range prefixes {
				prefix := uriPrefix(prefixStr)
				runtime := newRuntime[prefix]
				runtime.McpServer = &mcpServer

				var transport mcpproxy.Transport
				if oldRuntime, exists := oldState.Runtime[prefix]; exists {
					oldConfig := oldRuntime.McpServer
					if oldConfig != nil && oldConfig.Type == mcpServer.Type &&
						oldConfig.Command == mcpServer.Command &&
						oldConfig.URL == mcpServer.URL &&
						len(oldConfig.Args) == len(mcpServer.Args) {
						argsMatch := true
						for i, arg := range oldConfig.Args {
							if arg != mcpServer.Args[i] {
								argsMatch = false
								break
							}
						}
						if argsMatch {
							transport = oldRuntime.Transport
						}
					}
				}

				if transport == nil {
					var err error
					transport, err = mcpproxy.NewTransport(mcpServer)
					if err != nil {
						return nil, fmt.Errorf("failed to create transport for server %s: %w", mcpServer.Name, err)
					}
				}

				if mcpServer.Policy == cnst.PolicyOnStart {
					go startMCPServer(ctx, logger, string(prefix), mcpServer, transport, false)
				} else if mcpServer.Preinstalled {
					go startMCPServer(ctx, logger, string(prefix), mcpServer, transport, true)
				}
				runtime.Transport = transport

				switch mcpServer.Type {
				case cnst.BackendProtoStdio.String():
					runtime.ProtoType = cnst.BackendProtoStdio
				case cnst.BackendProtoSSE.String():
					runtime.ProtoType = cnst.BackendProtoSSE
				case cnst.BackendProtoStreamable.String():
					runtime.ProtoType = cnst.BackendProtoStreamable
				}
				newRuntime[prefix] = runtime
			}
		}
	}

	// Recalculate metrics from scratch to ensure accuracy.
	newMetrics := metrics{}
	allTools := make(map[string]bool)
	allHTTPServers := make(map[string]bool)
	allMCPServers := make(map[string]bool)

	for _, runtime := range newRuntime {
		for toolName := range runtime.Tools {
			allTools[string(toolName)] = true
		}
		if runtime.Server != nil {
			allHTTPServers[runtime.Server.Name] = true
		}
		if runtime.McpServer != nil {
			allMCPServers[runtime.McpServer.Name] = true
		}
	}
	newMetrics.TotalTools = len(allTools)
	newMetrics.HttpServers = len(allHTTPServers)
	newMetrics.McpServers = len(allMCPServers)
	// Note: Idle server and missing tool metrics are harder to calculate here
	// and may need a more sophisticated approach if they are critical.
	// For now, we focus on the primary counts.

	// Atomically swap the runtime map and update metrics
	oldState.Runtime = newRuntime
	oldState.Metrics = newMetrics

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
