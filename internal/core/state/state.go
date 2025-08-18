package state

import (
	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/core/mcpproxy"
	"mcp-gateway/pkg/mcp"
	"sync"
)

type (
	uriPrefix  string
	toolName   string
	promptName string

	// State contains all the read-only shared state
	State struct {
		RawConfigs []*config.MCPConfig       `json:"rawConfigs"`
		Runtime    map[uriPrefix]runtimeUnit `json:"runtime"`
		Metrics    metrics                   `json:"metrics"`
		mu         sync.RWMutex
	}

	runtimeUnit struct {
		ProtoType      cnst.ProtoType                      `json:"protoType"`
		Router         *config.RouterConfig                `json:"router"`
		Server         *config.ServerConfig                `json:"server"`
		McpServer      *config.MCPServerConfig             `json:"mcpServer"`
		Transport      mcpproxy.Transport                  `json:"-"`
		Tools          map[toolName]*config.ToolConfig     `json:"tools"`
		ToolSchemas    []mcp.ToolSchema                    `json:"toolSchemas"`
		Prompts        map[promptName]*config.PromptConfig `json:"prompts"`
		PromptSchemas  []mcp.PromptSchema                  `json:"promptSchemas"`
		ConsumerTokens []string                            `json:"consumerTokens,omitempty"`
	}

	metrics struct {
		TotalTools      int `json:"totalTools"`
		MissingTools    int `json:"missingTools"`
		HttpServers     int `json:"httpServers"`
		McpServers      int `json:"mcpServers"`
		IdleHTTPServers int `json:"idleHttpServers"`
		IdleMCPServers  int `json:"idleMcpServers"`
	}
)

func NewState() *State {
	return &State{
		RawConfigs: make([]*config.MCPConfig, 0),
		Runtime:    make(map[uriPrefix]runtimeUnit),
		Metrics:    metrics{},
	}
}
