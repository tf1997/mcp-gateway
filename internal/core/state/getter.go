package state

import (
	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/core/mcpproxy"
	"mcp-gateway/pkg/mcp"
)

func (s *State) getRuntime(prefix string) runtimeUnit {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return runtimeUnit{
			Tools:         make(map[toolName]*config.ToolConfig),
			ToolSchemas:   make([]mcp.ToolSchema, 0),
			Prompts:       make(map[promptName]*config.PromptConfig),
			PromptSchemas: make([]mcp.PromptSchema, 0),
		}
	}
	return runtime
}

func (s *State) setRouter(prefix string, router *config.RouterConfig) {
	runtime := s.getRuntime(prefix)
	runtime.Router = router
	s.Runtime[uriPrefix(prefix)] = runtime
}

func (s *State) GetCORS(prefix string) *config.CORSConfig {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if ok && runtime.Router != nil {
		return runtime.Router.CORS
	}
	return nil
}

func (s *State) GetRouterCount() int {
	count := 0
	for _, runtime := range s.Runtime {
		if runtime.Router != nil {
			count++
		}
	}
	return count
}

func (s *State) GetToolCount() int {
	return s.Metrics.totalTools
}

func (s *State) GetMissingToolCount() int {
	return s.Metrics.missingTools
}

func (s *State) GetServerCount() int {
	count := 0
	for _, runtime := range s.Runtime {
		if runtime.Server != nil {
			count++
		}
	}
	return count
}

func (s *State) GetTool(prefix, name string) *config.ToolConfig {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return nil
	}
	return runtime.Tools[toolName(name)]
}

func (s *State) GetToolSchemas(prefix string) []mcp.ToolSchema {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return nil
	}
	return runtime.ToolSchemas
}

func (s *State) GetServerConfig(prefix string) *config.ServerConfig {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return nil
	}
	return runtime.Server
}

func (s *State) GetProtoType(prefix string) cnst.ProtoType {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return ""
	}
	return runtime.ProtoType
}

func (s *State) GetTransport(prefix string) mcpproxy.Transport {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return nil
	}
	return runtime.Transport
}

func (s *State) GetTransports() map[string]mcpproxy.Transport {
	transports := make(map[string]mcpproxy.Transport)
	for prefix, runtime := range s.Runtime {
		if runtime.Transport != nil {
			transports[string(prefix)] = runtime.Transport
		}
	}
	return transports
}

func (s *State) GetRawConfigs() []*config.MCPConfig {
	return s.RawConfigs
}

func (s *State) GetAuth(prefix string) *config.Auth {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok || runtime.Router == nil {
		return nil
	}
	return runtime.Router.Auth
}

func (s *State) GetSSEPrefix(prefix string) string {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if ok && runtime.Router != nil {
		return runtime.Router.SSEPrefix
	}
	return ""
}

func (s *State) GetPrompt(prefix, name string) *config.PromptConfig {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return nil
	}
	return runtime.Prompts[promptName(name)]
}

func (s *State) GetPromptSchemas(prefix string) []mcp.PromptSchema {
	runtime, ok := s.Runtime[uriPrefix(prefix)]
	if !ok {
		return nil
	}
	return runtime.PromptSchemas
}
