#!/bin/bash

# Configuration and setup for proxy testing

set -e

echo "🔧 Setting up test environment for apipod-smart-proxy..."

# Check if proxy is running
check_proxy() {
    if ! curl -s http://localhost:8081/health > /dev/null 2>&1; then
        echo "❌ Proxy not running on localhost:8081"
        echo "💡 Start the proxy first with: go run cmd/server/main.go"
        exit 1
    fi
    echo "✅ Proxy is running on localhost:8081"
}

# Check dependencies
check_deps() {
    if ! command -v jq &> /dev/null; then
        echo "❌ jq is required for JSON parsing"
        echo "💡 Install with: brew install jq (macOS) or apt install jq (Ubuntu)"
        exit 1
    fi
    echo "✅ jq is available"
    
    if ! command -v curl &> /dev/null; then
        echo "❌ curl is required for API requests"
        exit 1
    fi
    echo "✅ curl is available"
}

# Setup test files
setup_test_files() {
    echo "📄 Creating test files..."
    
    # Create test README if it doesn't exist
    if [ ! -f "README.md" ]; then
        cat > README.md << 'EOF'
# apipod-smart-proxy Test

This is a test README file for proxy tool execution testing.

## Features
- Tool execution
- Token optimization  
- API routing

## Dependencies
- Go 1.24+
- PostgreSQL
EOF
        echo "✅ Created test README.md"
    fi
    
    # Create test config if needed
    if [ ! -f "test_data.txt" ]; then
        cat > test_data.txt << 'EOF'
Line 1: This is test data
Line 2: Used for testing Read tool
Line 3: With offset and limit parameters
Line 4: Final line for testing
EOF
        echo "✅ Created test_data.txt"
    fi
}

# Configure API key
setup_api_key() {
    if [ -z "$ANTHROPIC_API_KEY" ]; then
        echo "⚠️  ANTHROPIC_API_KEY not set in environment"
        echo "💡 Set with: export ANTHROPIC_API_KEY=your-key-here"
        echo "💡 Or edit test_proxy.sh to hardcode the key"
    else
        echo "✅ API key found in environment"
        # Update test script with actual API key
        sed -i.bak "s/sk-GWALtxPQJ32kynNSX19OrIOTP6mTkKT6FxyES9aw79DWqW6s/$ANTHROPIC_API_KEY/" test_proxy.sh
        echo "✅ Updated test_proxy.sh with API key"
    fi
}

# Main setup
main() {
    echo "🚀 apipod-smart-proxy Test Setup"
    echo "================================="
    
    check_deps
    check_proxy
    setup_test_files
    setup_api_key
    
    echo ""
    echo "✅ Setup complete! Ready to run tests."
    echo ""
    echo "🚦 Next steps:"
    echo "1. Run basic tests: ./test_proxy.sh"
    echo "2. Check logs: tail -f runner.log"
    echo "3. Monitor tool execution in proxy logs"
    echo ""
    echo "🔍 What to look for:"
    echo "   - [tool_execution] log entries"
    echo "   - Reduced token usage (2-8KB vs 60KB+)"
    echo "   - Successful tool responses"
}

main "$@"