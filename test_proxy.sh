#!/bin/bash

# Test script for apipod-smart-proxy tool execution
# Simulates Claude Code making requests

set -e

# Configuration
PROXY_URL="http://localhost:8081"
API_KEY="sk-GWALtxPQJ32kynNSX19OrIOTP6mTkKT6FxyES9aw79DWqW6s"  # Replace with actual API key
MODEL="claude-sonnet-4-6"    # Adjust model as needed

echo "🚀 Testing apipod-smart-proxy tool execution..."
echo "Proxy URL: $PROXY_URL"
echo "Model: $MODEL"
echo ""

# Test 1: Simple query (no tools)
echo "📝 Test 1: Simple query (no tools)"
echo "Request: What is 2+2?"

response1=$(curl -s -X POST "$PROXY_URL/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "'$MODEL'",
    "max_tokens": 100,
    "messages": [
      {
        "role": "user",
        "content": "What is 2+2? Just give me the answer."
      }
    ]
  }' | jq -r '.content[0].text // "Error: No response"')

echo "Response: $response1"
echo ""

# Test 2: Tool usage - Read a file
echo "🔧 Test 2: Tool execution - Read README.md"
echo "Request: Read the README.md file"

# Create a simple README for testing if it doesn't exist
if [ ! -f "README.md" ]; then
    echo "Creating test README.md..."
    echo "# Test README\nThis is a test file for proxy tool execution.\nLine 3\nLine 4" > README.md
fi

response2=$(curl -s -X POST "$PROXY_URL/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "'$MODEL'",
    "max_tokens": 1000,
    "messages": [
      {
        "role": "user",
        "content": "Read the README.md file in the current directory and tell me what it contains."
      }
    ],
    "tools": [
      {
        "name": "Read",
        "description": "Read file contents",
        "input_schema": {
          "type": "object", 
          "properties": {
            "file_path": {"type": "string"}
          },
          "required": ["file_path"]
        }
      }
    ]
  }')

echo "Response:"
echo "$response2" | jq -r '.content[0].text // "Error: No text response"'
echo ""

# Test 3: Tool usage - Bash command
echo "⚙️  Test 3: Tool execution - Bash command"
echo "Request: List files in current directory"

response3=$(curl -s -X POST "$PROXY_URL/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "'$MODEL'",
    "max_tokens": 1000,
    "messages": [
      {
        "role": "user",
        "content": "List all files in the current directory using ls command."
      }
    ],
    "tools": [
      {
        "name": "Bash",
        "description": "Execute bash commands",
        "input_schema": {
          "type": "object",
          "properties": {
            "command": {"type": "string"}
          },
          "required": ["command"]
        }
      }
    ]
  }')

echo "Response:"
echo "$response3" | jq -r '.content[0].text // "Error: No text response"'
echo ""

# Test 4: Complex tool chain
echo "🔗 Test 4: Complex tool usage - Read + Analysis"
echo "Request: Read go.mod and analyze dependencies"

response4=$(curl -s -X POST "$PROXY_URL/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "'$MODEL'",
    "max_tokens": 1500,
    "messages": [
      {
        "role": "user",
        "content": "Read the go.mod file and tell me what dependencies this project has."
      }
    ],
    "tools": [
      {
        "name": "Read",
        "description": "Read file contents",
        "input_schema": {
          "type": "object",
          "properties": {
            "file_path": {"type": "string"}
          },
          "required": ["file_path"]
        }
      },
      {
        "name": "Glob",
        "description": "Find files matching pattern",
        "input_schema": {
          "type": "object",
          "properties": {
            "pattern": {"type": "string"}
          },
          "required": ["pattern"]
        }
      }
    ]
  }')

echo "Response:"
echo "$response4" | jq -r '.content[0].text // "Error: No text response"'
echo ""

# Test 5: Error handling
echo "❌ Test 5: Error handling - Read non-existent file"
echo "Request: Read non-existent file"

response5=$(curl -s -X POST "$PROXY_URL/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "'$MODEL'",
    "max_tokens": 500,
    "messages": [
      {
        "role": "user",
        "content": "Read the file that-does-not-exist.txt"
      }
    ],
    "tools": [
      {
        "name": "Read",
        "description": "Read file contents",
        "input_schema": {
          "type": "object",
          "properties": {
            "file_path": {"type": "string"}
          },
          "required": ["file_path"]
        }
      }
    ]
  }')

echo "Response:"
echo "$response5" | jq -r '.content[0].text // "Error: No text response"'
echo ""

echo "✅ All tests completed!"
echo ""
echo "💡 Check your proxy logs (runner.log) to see:"
echo "   - Token usage optimization"
echo "   - Tool execution logging"
echo "   - Performance improvements"