# GHCSD

A HTTP server that proxies OpenAI-style API requests to GitHub Copilot by adding required headers. This allows you to use GitHub Copilot's capabilities through an OpenAI-compatible API interface.

## Features

- OpenAI API compatibility for chat completions
- Support for multiple models including GPT-4, Claude 3.5 Sonnet, and more
- Streaming and non-streaming response support
- Secure token management with automatic refresh
- Debug mode for request/response logging
- Rate limiting and error handling
- Easy configuration via environment variables
- Docker support

## Prerequisites

- Go 1.23.2 or later (for local build)
- GitHub Copilot subscription
- Valid GitHub account
- Docker (optional, for containerized deployment)

## Supported Models

The following models are supported:
- `gpt-4` or `4`: Standard GPT-4 model
- `gpt-4o` or `4o`: Optimized GPT-4 model (default)
- `o1-mini`: OpenAI's smaller model
- `o1-preview`: OpenAI's preview model
- `sonnet` or `claude-3.5-sonnet`: Claude 3.5 Sonnet model

## Installation

### Local Installation

1. Clone the repository:
```bash
git clone https://github.com/acazau/ghcsd
cd ghcsd
```

2. Install dependencies:
```bash
go mod download
```

3. Build and install the project:
```bash
go install github.com/acazau/ghcsd/cmd/server@latest
```

   This will install the `ghcsd` executable in your `$GOPATH/bin` directory (or `$HOME/go/bin` if you're using Go modules).

   Alternatively, you can build the project directly:
```bash
go build -o ghcsd cmd/server/main.go
```

### Docker Installation

1. Clone the repository:
```bash
git clone https://github.com/acazau/ghcsd
cd ghcsd
```

2. Build the Docker image:
```bash
docker build -t ghcsd .
```

The Docker image is optimized for size and security:
- Uses multi-stage building
- Final image is based on `scratch` (empty base image)
- Contains only the essential static binary and SSL certificates
- Typically results in an image size of less than 20MB

## Configuration

The server uses the following configuration:
- Default Server Address: `:8080`
- Default Model: `gpt-4o`
- Config Directory: `~/.config/ghcsd/`
- Auth Token Path: `~/.config/ghcsd/.copilot-auth-token`

## Authentication

The server implements GitHub's device code flow for authentication:
1. On first run, the server will request device authorization
2. You'll be provided with a URL and code to enter on GitHub
3. After authorization, tokens are securely stored in the config directory
4. Tokens are automatically refreshed as needed

## Usage

### Running Locally

1. Start the server:
```bash
./ghcsd
```

Or with debug mode:
```bash
DEBUG=1 ./ghcsd
```

### Running with Docker Compose

The project includes a `docker-compose.yml` file that provides a production-ready setup with:
- Health checks
- Log rotation
- Volume management
- Security hardening
- Automatic restarts

#### Basic Usage

1. Start the service:
```bash
# Start in daemon mode
docker-compose up -d

# Start with logs
docker-compose up

# Start with debug mode
DEBUG=1 docker-compose up
```

2. View logs:
```bash
# Follow logs
docker-compose logs -f

# View last N lines
docker-compose logs --tail=100

# View logs with timestamps
docker-compose logs -f -t
```

3. Stop the service:
```bash
# Stop while preserving volumes
docker-compose down

# Stop and remove volumes
docker-compose down -v
```

#### Managing the Service

Restart the service:
```bash
docker-compose restart
```

Scale the service (if needed):
```bash
docker-compose up -d --scale ghcsd=2
```

Check service health:
```bash
# View container status
docker-compose ps

# Check health status
docker inspect ghcsd | jq '.[0].State.Health'
```

#### Volume Management

The service uses a named volume `ghcsd_config` to persist data:

```bash
# Create volume explicitly
docker volume create ghcsd_config

# List volumes
docker volume ls

# Inspect volume
docker volume inspect ghcsd_config

# Remove volume (caution: deletes all data)
docker volume rm ghcsd_config
```

#### Maintenance

Update to latest version:
```bash
# Pull latest changes
git pull

# Rebuild container
docker-compose build

# Restart with new version
docker-compose up -d
```

Clean up unused resources:
```bash
# Remove unused containers
docker-compose rm -f

# Remove unused volumes
docker volume prune

# Remove unused images
docker image prune
```

#### Environment Variables

You can customize the service using environment variables:

```bash
# Set debug mode
DEBUG=1 docker-compose up -d

# Change server port
PORT=8080 docker-compose up -d
```

Or by creating a `.env` file:
```env
DEBUG=1
PORT=8080
```

#### Running Multiple Instances

To run multiple instances with different configurations:

1. Create a new compose file (e.g., `docker-compose.debug.yml`):
```yaml
version: '3.8'
services:
  ghcsd:
    environment:
      - DEBUG=1
    ports:
      - "8081:8080"
```

2. Run with specific compose file:
```bash
docker-compose -f docker-compose.yml -f docker-compose.debug.yml up -d
```

2. The server exposes a single endpoint:
- POST `/v1/chat/completions`

### Example Usage

Using curl:
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {
        "role": "user",
        "content": "Hello, how are you?"
      }
    ],
    "stream": false
  }'
```

Using Python with the OpenAI library:
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="dummy"  # The key is not used but required
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[
        {"role": "user", "content": "Hello, how are you?"}
    ]
)
print(response.choices[0].message.content)
```

## Project Structure

```
ghcsd/
├── cmd/
│   └── server/
│       └── main.go           # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go         # Configuration management
│   ├── copilot/
│   │   ├── auth.go          # GitHub authentication
│   │   ├── client.go        # Copilot API client
│   │   └── types.go         # Type definitions
│   └── proxy/
│       └── handler.go        # HTTP request handler
├── Dockerfile               # Docker configuration
├── docker-compose.yml       # Docker Compose configuration
├── go.mod                   # Go module file
└── README.md               # Documentation
```

## Docker Volumes

When running with Docker, the application uses a named volume `ghcsd_config` to persist authentication data. This ensures your authentication tokens are preserved between container restarts.

To manage the volume:
- Create: `docker volume create ghcsd_config`
- Inspect: `docker volume inspect ghcsd_config`
- Remove: `docker volume rm ghcsd_config`

## Error Handling

The server implements comprehensive error handling:
- Invalid requests return appropriate HTTP status codes
- Authentication failures trigger automatic token refresh
- Network errors are handled gracefully
- Detailed debug logging when enabled

## Security Features

- Secure token storage with appropriate file permissions
- Token masking in debug logs
- Local-only server by default
- Request ID tracking
- Secure random number generation for session IDs
- Minimal Docker container based on scratch image
- Statically compiled binary with no dependencies
- Minimal attack surface with only essential components

## Debug Mode

Enable debug logging by setting the DEBUG environment variable:
```bash
# Local
DEBUG=1 ./ghcsd

# Docker
docker run -p 8080:8080 -e DEBUG=1 -v ghcsd_config:/root/.config/ghcsd ghcsd
```

Debug mode provides detailed logging of:
- Request headers and bodies
- Response data
- Authentication processes
- Token management
- Error details

## Common Issues & Troubleshooting

1. **Authentication Failures**
   - Ensure you have an active GitHub Copilot subscription
   - Check the auth token file permissions
   - Try removing the auth token to trigger re-authentication
   - For Docker: ensure the volume is properly mounted

2. **Connection Issues**
   - Verify the server is running
   - Check if the port is available
   - Ensure you're using the correct base URL
   - For Docker: check port mapping and network settings

3. **Rate Limiting**
   - Copilot has built-in rate limiting
   - Implement appropriate retry logic in your client
   - Consider using streaming for long responses

4. **Docker-specific Issues**
   - Volume permissions: ensure the volume has correct permissions
   - Container networking: verify port mappings
   - Resource constraints: check container resource limits

## License

This project is licensed under the MIT License.
