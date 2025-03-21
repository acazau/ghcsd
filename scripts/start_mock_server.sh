#!/bin/bash

# Create a temporary directory for the OpenAPI spec
TEMP_DIR=$(mktemp -d -t prism-test-XXXXXX)
SPEC_PATH="$TEMP_DIR/anthropic-api.yaml"

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
FIXTURES_DIR="$SCRIPT_DIR/../internal/proxy/anthropic/fixtures"

# Write OpenAPI spec
cat > "$SPEC_PATH" << 'EOL'
openapi: 3.1.0
info:
  title: Anthropic API
  version: 1.0.0
servers:
  - url: https://api.anthropic.com
paths:
  /:
    get:
      summary: Root endpoint
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                type: object
  /anthropic/health:
    get:
      summary: Health check endpoint
      responses:
        '200':
          description: Health check response
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                  message:
                    type: string
                example:
                  status: "ok"
                  message: "Anthropic API proxy is healthy"
  /v1/messages:
    post:
      summary: Create a message
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                stream:
                  type: boolean
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Message"
              examples:
                simple:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/test_responses.json#/simple"
                calculator:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/test_responses.json#/calculator"
                multi_turn:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/test_responses.json#/multi_turn"
            text/event-stream:
              schema:
                type: string
              examples:
                simple_stream:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/streaming_responses.json#/simple_stream"
                calculator_stream:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/streaming_responses.json#/calculator_stream"
  /v1/messages/count_tokens:
    post:
      summary: Count tokens in a message
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
                properties:
                  input_tokens:
                    type: integer
                    minimum: 1
              examples:
                simple_text:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/token_count_responses.json#/simple_text"
                with_system:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/token_count_responses.json#/with_system"
                with_tools:
                  value:
                    $ref: "../internal/proxy/anthropic/fixtures/token_count_responses.json#/with_tools"
components:
  schemas:
    Message:
      type: object
      properties:
        id:
          type: string
        type:
          type: string
        role:
          type: string
        content:
          type: array
          items:
            oneOf:
              - type: object
                properties:
                  type:
                    type: string
                    enum: [text]
                  text:
                    type: string
              - type: object
                properties:
                  type:
                    type: string
                    enum: [tool_use]
                  tool_calls:
                    type: array
                    items:
                      type: object
        model:
          type: string
        stop_reason:
          type: string
      required:
        - id
        - type
        - role
        - content
EOL

# Check if prism is installed
if ! command -v prism &> /dev/null; then
    echo "Error: Prism is not installed. Please install it first:"
    echo "npm install -g @stoplight/prism-cli"
    exit 1
fi

# Export the mock server URL for tests to use
export MOCK_SERVER_URL="http://127.0.0.1:4010"

echo "Starting Prism mock server on $MOCK_SERVER_URL"
echo "API spec location: $SPEC_PATH"
echo "Using fixtures from: $FIXTURES_DIR"

# Start Prism mock server with host binding to all interfaces and dynamic mocking
prism mock --host 0.0.0.0 -p 4010 "$SPEC_PATH" --dynamic &

# Wait for the server to start
echo "Waiting for mock server to start..."
sleep 2

# Check if server is running
if curl -s -f "$MOCK_SERVER_URL/anthropic/health" > /dev/null 2>&1; then
    echo "Mock server is running"
else
    echo "Failed to start mock server"
    exit 1
fi

# Clean up temp directory on script exit
trap "kill %1; rm -rf $TEMP_DIR" EXIT

# Keep script running
wait