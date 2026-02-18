package tools

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Executor struct {
	logger *log.Logger
}

func NewExecutor(logger *log.Logger) *Executor {
	return &Executor{logger: logger}
}

type ToolCall struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

func (e *Executor) ExecuteTool(call ToolCall) ToolResult {
	startTime := time.Now()
	e.logger.Printf("[tools] starting execution of %s (id=%s)", call.Name, call.ID)
	
	var result ToolResult
	switch call.Name {
	case "Read":
		result = e.executeRead(call)
	case "Bash":
		result = e.executeBash(call)
	case "Write":
		result = e.executeWrite(call)
	case "Edit":
		result = e.executeEdit(call)
	case "Glob":
		result = e.executeGlob(call)
	case "Grep":
		result = e.executeGrep(call)
	// Handle common tool name variations
	case "google:list_files", "list_files", "ls":
		result = e.executeGlob(ToolCall{ID: call.ID, Name: "Glob", Input: map[string]interface{}{"pattern": "*"}})
	case "cat", "read_file":
		result = e.executeRead(call)
	default:
		result = ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Tool %s not implemented", call.Name),
			IsError:   true,
		}
	}
	
	duration := time.Since(startTime)
	status := "success"
	if result.IsError {
		status = "error"
	}
	e.logger.Printf("[tools] completed %s (id=%s) in %v - %s", call.Name, call.ID, duration, status)
	return result
}

func (e *Executor) executeRead(call ToolCall) ToolResult {
	filePath, ok := call.Input["file_path"].(string)
	if !ok {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameter: file_path",
			IsError:   true,
		}
	}
	
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Error reading file: %v", err),
			IsError:   true,
		}
	}
	
	lines := strings.Split(string(content), "\n")
	
	// Handle offset and limit parameters
	offset := 0
	limit := len(lines)
	
	if offsetVal, ok := call.Input["offset"]; ok {
		if offsetFloat, ok := offsetVal.(float64); ok {
			offset = int(offsetFloat) - 1 // Convert to 0-based indexing
			if offset < 0 {
				offset = 0
			}
		}
	}
	
	if limitVal, ok := call.Input["limit"]; ok {
		if limitFloat, ok := limitVal.(float64); ok {
			requestedLimit := int(limitFloat)
			if requestedLimit > 0 {
				limit = offset + requestedLimit
			}
		}
	}
	
	// Ensure bounds
	if offset >= len(lines) {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Offset beyond file length",
			IsError:   true,
		}
	}
	if limit > len(lines) {
		limit = len(lines)
	}
	
	var result strings.Builder
	for i := offset; i < limit; i++ {
		result.WriteString(fmt.Sprintf("%5d→%s\n", i+1, lines[i]))
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   result.String(),
	}
}

func (e *Executor) executeBash(call ToolCall) ToolResult {
	command, ok := call.Input["command"].(string)
	if !ok {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameter: command",
			IsError:   true,
		}
	}
	
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = "." // Current working directory
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(output)),
			IsError:   true,
		}
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   string(output),
	}
}

func (e *Executor) executeWrite(call ToolCall) ToolResult {
	filePath, ok := call.Input["file_path"].(string)
	if !ok {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameter: file_path",
			IsError:   true,
		}
	}
	
	content, ok := call.Input["content"].(string)
	if !ok {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameter: content",
			IsError:   true,
		}
	}
	
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Error writing file: %v", err),
			IsError:   true,
		}
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   fmt.Sprintf("File written successfully: %s", filePath),
	}
}

func (e *Executor) executeEdit(call ToolCall) ToolResult {
	filePath, _ := call.Input["file_path"].(string)
	oldString, _ := call.Input["old_string"].(string) 
	newString, _ := call.Input["new_string"].(string)
	
	if filePath == "" || oldString == "" {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameters: file_path, old_string",
			IsError:   true,
		}
	}
	
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Error reading file: %v", err),
			IsError:   true,
		}
	}
	
	if !strings.Contains(string(content), oldString) {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "String not found in file",
			IsError:   true,
		}
	}
	
	newContent := strings.Replace(string(content), oldString, newString, 1)
	err = os.WriteFile(filePath, []byte(newContent), 0644)
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Error writing file: %v", err),
			IsError:   true,
		}
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   fmt.Sprintf("File edited successfully: %s", filePath),
	}
}

func (e *Executor) executeGlob(call ToolCall) ToolResult {
	pattern, ok := call.Input["pattern"].(string)
	if !ok {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameter: pattern",
			IsError:   true,
		}
	}
	
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Glob error: %v", err),
			IsError:   true,
		}
	}
	
	if len(matches) == 0 {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "No files found",
		}
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   strings.Join(matches, "\n"),
	}
}

func (e *Executor) executeGrep(call ToolCall) ToolResult {
	pattern, ok := call.Input["pattern"].(string)
	if !ok {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   "Missing required parameter: pattern",
			IsError:   true,
		}
	}
	
	// Use ripgrep if available
	cmd := exec.Command("rg", pattern, ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Grep failed: %v", err),
			IsError:   true,
		}
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   string(output),
	}
}