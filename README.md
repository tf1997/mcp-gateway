# 🚀 MCP Gateway

Welcome to the **MCP Gateway** project! This repository hosts a robust and efficient gateway designed to facilitate seamless communication and interaction within the Model Context Protocol (MCP) ecosystem. 🌐

## ✨ Features

*   **Secure Authentication**: Integrates with OAuth2 and JWT for secure access control. 🔐
*   **Flexible Configuration**: Easily configurable via YAML. ⚙️
*   **State Management**: Persistent state handling for reliable operations. 💾
*   **Internationalization (i18n)**: Supports multiple languages for broader accessibility. 🌍
*   **MCP Proxy**: Handles MCP-specific protocols and interactions. 🤝
*   **API Server**: Provides a robust API for external services. 🔗

## 🛠️ Getting Started

### Prerequisites

*   Go (version 1.18 or higher recommended)
*   Redis (for session and auth storage, if configured)

### Installation

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/tf1997/mcp-gateway.git
    cd mcp-gateway
    ```
2.  **Install dependencies**:
    ```bash
    go mod tidy
    ```

### Configuration

The gateway can be configured using `configs/mcp-gateway.yaml` and other JSON configuration files in the `configs/` directory. Adjust these files to suit your environment and requirements.

### Running the Gateway

To start the MCP Gateway:

```bash
go run cmd/main.go -c ./configs/mcp-gateway.yaml
```

## 🌐 API Usage

The MCP Gateway exposes a configuration API at `http://127.0.0.1:5235/api/v1/configs` for managing router configurations.

### Add/Update Configuration (POST)

```bash
curl -X POST \
  'http://127.0.0.1:5235/api/v1/configs' \
  --header 'Accept: */*' \
  --header 'User-Agent: HTTP Client' \
  --header 'Content-Type: application/json' \
  --data-raw '[
  {
    "name": "test",
    "tenant": "",
    "createdAt": "2025-08-09T00:00:00Z",
    "updatedAt": "2025-08-09T00:00:00Z",
    "routers": [
      {
        "server": "baidu",
        "prefix": "/gateway/baidu",
        "ssePrefix": "",
        "cors": {
          "allowOrigins": [],
          "allowMethods": [],
          "allowHeaders": [],
          "exposeHeaders": [],
          "allowCredentials": false
        }
      }
    ],
    "servers": [
      {
        "name": "baidu",
        "description": "Baidu search service",
        "allowedTools": [
          "search"
        ],
        "config": {}
      }
    ],
    "tools": [
      {
        "name": "search",
        "description": "Baidu search service, search by keyword",
        "method": "GET",
        "endpoint": "https://www.baidu.com/s",
        "headers": {
          "Content-Type": "application/json"
        },
        "args": [
          {
            "name": "wd",
            "position": "query",
            "required": true,
            "type": "string",
            "description": "Search content",
            "default": "",
            "items": {
              "type": "",
              "enum": [],
              "properties": {},
              "items": null,
              "required": []
            }
          }
        ],
        "requestBody": "",
        "responseBody": "{{.Response.Body}}",
        "inputSchema": {}
      }
    ]
  }
]'
```

### Retrieve Configurations (GET)

```bash
curl -X GET \
  'http://127.0.0.1:5235/api/v1/configs' \
  --header 'Accept: */*' \
  --header 'User-Agent: HTTP Client'
```

### Delete Configuration (DELETE)

```bash
curl -X DELETE \
  'http://127.0.0.1:5235/api/v1/configs' \
  --header 'Accept: */*' \
  --header 'User-Agent: HTTP Client' \
  --header 'Content-Type: application/json' \
  --data-raw '[
  "/gateway/baidu"
]'
```

## 🤝 Contributing
Contributions are welcome! Please feel free to open issues or submit pull requests.

## 📄 License

This project is licensed under the MIT License. See the `LICENSE` file for more details.

---
Made with ❤️ by the MCP Gateway Team
