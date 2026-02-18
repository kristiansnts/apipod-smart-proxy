#!/bin/bash

# Realistic Claude Code simulation test
# Tests tool execution exactly like Claude Code would use it

set -e

PROXY_URL="http://localhost:8081"
API_KEY="${ANTHROPIC_API_KEY:-sk-GWALtxPQJ32kynNSX19OrIOTP6mTkKT6FxyES9aw79DWqW6s}"
MODEL="claude-sonnet-4-6"

echo "🤖 Claude Code Simulation Test"
echo "============================="
echo "This simulates exactly how Claude Code would use the proxy"
echo ""

# Function to make API call with full Claude Code tool set
make_claude_request() {
    local message="$1"
    local test_name="$2"
    
    echo "📤 $test_name"
    echo "Message: $message"
    echo ""
    
    curl -s -X POST "$PROXY_URL/v1/messages" \
      -H "Content-Type: application/json" \
      -H "x-api-key: $API_KEY" \
      -H "anthropic-version: 2023-06-01" \
      -d '{
        "model": "'$MODEL'",
        "max_tokens": 2000,
        "messages": [{"role": "user", "content": "'$message'"}],
        "tools": [
          {
            "name": "Read",
            "description": "Reads a file from the local filesystem",
            "input_schema": {
              "type": "object",
              "properties": {
                "file_path": {"type": "string", "description": "The absolute path to the file to read"},
                "limit": {"type": "number", "description": "The number of lines to read"},
                "offset": {"type": "number", "description": "The line number to start reading from"}
              },
              "required": ["file_path"]
            }
          },
          {
            "name": "Bash",
            "description": "Execute bash commands with timeout support",
            "input_schema": {
              "type": "object", 
              "properties": {
                "command": {"type": "string", "description": "The command to execute"},
                "description": {"type": "string", "description": "Clear, concise description of what this command does"},
                "timeout": {"type": "number", "description": "Optional timeout in milliseconds"}
              },
              "required": ["command"]
            }
          },
          {
            "name": "Write",
            "description": "Writes a file to the local filesystem",
            "input_schema": {
              "type": "object",
              "properties": {
                "file_path": {"type": "string", "description": "The absolute path to the file to write"},
                "content": {"type": "string", "description": "The content to write to the file"}
              },
              "required": ["file_path", "content"]
            }
          },
          {
            "name": "Edit",
            "description": "Performs exact string replacements in files",
            "input_schema": {
              "type": "object",
              "properties": {
                "file_path": {"type": "string", "description": "The absolute path to the file to modify"},
                "old_string": {"type": "string", "description": "The text to replace"},
                "new_string": {"type": "string", "description": "The text to replace it with"},
                "replace_all": {"type": "boolean", "description": "Replace all occurences"}
              },
              "required": ["file_path", "old_string", "new_string"]
            }
          },
          {
            "name": "Glob",
            "description": "Fast file pattern matching tool",
            "input_schema": {
              "type": "object",
              "properties": {
                "pattern": {"type": "string", "description": "The glob pattern to match files against"},
                "path": {"type": "string", "description": "The directory to search in"}
              },
              "required": ["pattern"]
            }
          },
          {
            "name": "Grep",
            "description": "A powerful search tool built on ripgrep",
            "input_schema": {
              "type": "object",
              "properties": {
                "pattern": {"type": "string", "description": "The regular expression pattern to search for"},
                "output_mode": {"type": "string", "enum": ["content", "files_with_matches", "count"], "description": "Output mode"},
                "glob": {"type": "string", "description": "Glob pattern to filter files"},
                "path": {"type": "string", "description": "File or directory to search in"}
              },
              "required": ["pattern"]
            }
          }
        ]
      }' | jq -r '.content[]? | if .type == "text" then .text else if .type == "tool_use" then "🔧 Tool used: " + .name else . end end'
    
    echo ""
    echo "---"
    echo ""
}

# Test scenarios that Claude Code commonly uses

echo "Starting Claude Code simulation tests..."
echo ""

# Test 1: File exploration (common first action)
make_claude_request "List all Go files in this project to understand the codebase structure" "File Exploration"

# Test 2: Read specific file (very common)
make_claude_request "Read the go.mod file to see the dependencies" "Dependency Check"

# Test 3: Code analysis (typical request)
make_claude_request "Find all functions in the internal/tools/executor.go file and analyze the tool execution logic" "Code Analysis"

# Test 4: Search for patterns (common debugging)
make_claude_request "Search for all occurrences of 'tool_use' in the codebase to understand how tool calls are handled" "Pattern Search"

# Test 5: Build and test (development workflow)
make_claude_request "Build the project and run any tests to check if everything is working correctly" "Build & Test"

# Test 6: Error investigation (debugging scenario)
make_claude_request "Check the runner.log file for any error messages or warnings in the last 20 lines" "Log Investigation"

echo "✅ All Claude Code simulation tests completed!"
echo ""
echo "🔍 Check runner.log to verify:"
echo "   - Tool execution logged as [tool_execution] entries"
echo "   - Token usage reduced from 30K+ to <10K"
echo "   - Successful tool → response cycles"
echo ""
echo "📊 Expected improvements:"
echo "   - 90% token reduction"
echo "   - 10x faster responses"
echo "   - Full tool functionality"