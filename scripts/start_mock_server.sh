#!/bin/bash

cd "$(dirname "$0")/.."
SPEC_PATH="internal/proxy/anthropic/fixtures/anthropic-api.yaml"

# Write OpenAPI spec with evaluated paths
cat > "$SPEC_PATH" << EOF
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
                    enum: [ok]
                  message:
                    type: string
                required:
                  - status
                  - message
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
                \$ref: "#/components/schemas/Message"
              examples:
                simple:
                  value:
                    \$ref: "./test_responses.json#/simple"
                calculator:
                  value:
                    \$ref: "./test_responses.json#/calculator"
                multi_turn:
                  value:
                    \$ref: "./test_responses.json#/multi_turn"
            text/event-stream:
              schema:
                type: string
              examples:
                simple_stream:
                  value:
                    \$ref: "./streaming_responses.json#/simple_stream"
                calculator_stream:
                  value:
                    \$ref: "./streaming_responses.json#/calculator_stream"
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
                    \$ref: "./token_count_responses.json#/simple_text"
                with_system:
                  value:
                    \$ref: "./token_count_responses.json#/with_system"
                with_tools:
                  value:
                    \$ref: "./token_count_responses.json#/with_tools"
components:
  schemas:
    Message:
      type: object
      properties:
        id:
          type: string
        type:
          type: string
          enum: [message]
        role:
          type: string
          enum: [assistant]
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
                required:
                  - type
                  - text
              - type: object
                properties:
                  type:
                    type: string
                    enum: [tool_use]
                  tool_calls:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        type:
                          type: string
                        calculator:
                          type: object
                required:
                  - type
                  - tool_calls
        model:
          type: string
        stop_reason:
          type: string
          enum: [end_turn]
      required:
        - id
        - type
        - role
        - content
        - stop_reason
EOF

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

# Clean up on script exit
trap "kill %1" EXIT

# Keep script running
wait