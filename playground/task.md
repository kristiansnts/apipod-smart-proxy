# LLM Tool Integration Test Task

## Objective
Test that all LLM models can properly interact with the available tools by completing a comprehensive exploration and analysis task.

## Task Requirements

### 1. Project Exploration
- Use the **Glob** tool to find all Go files in the project
- Use the **Grep** tool to search for "func main" across all Go files
- Use the **Read** tool to examine the go.mod file and identify dependencies

### 2. Code Analysis
- Use the **Bash** tool to run `go mod tidy` and ensure dependencies are clean
- Use the **Read** tool to examine at least 3 key source files from internal/ directory
- Use the **Grep** tool to find all occurrences of "tool" in the codebase

### 3. File Operations
- Use the **Write** tool to create a new file `playground/test_output.txt`
- Use the **Edit** tool to modify an existing file (add a comment to go.mod)
- Use the **MultiEdit** tool to update multiple lines in a file (if applicable)

### 4. Task Management
- Use the **TodoWrite** tool to create a task list for this test
- Use the **Task** tool to delegate subtasks (if architecture supports it)

### 5. Web Integration Test
- Use the **WebFetch** tool to fetch the README from a public GitHub repository
- Use the **WebSearch** tool to search for "Go proxy patterns"

### 6. Advanced Operations
- Use the **BashOutput** tool to monitor a long-running process (if you start one)
- Use the **ExitPlanMode** tool when planning is complete

## Expected Deliverables

1. **Analysis Report**: Summary of project structure and key findings
2. **Tool Usage Log**: Documentation of which tools were successfully used
3. **Error Report**: Any issues encountered with specific tools
4. **Performance Notes**: Observations about tool response times and reliability

## Success Criteria
- At least 10 different tools from the "full" tool group should be used
- All file operations should complete without errors
- Web requests should return valid responses
- Task should demonstrate proper sequencing of tool calls

## Notes for LLM
- This playground directory is isolated - feel free to create/delete files here
- The proxy should handle tool execution transparently
- Document any authentication or configuration issues encountered
- Focus on testing tool compatibility rather than perfect code analysis

## Tool Reference (from tools/tool_groups.json)
Full tool set: Read, Edit, MultiEdit, Write, Glob, Grep, Bash, BashOutput, KillBash, Task, TodoWrite, WebFetch, WebSearch, NotebookEdit, ExitPlanMode