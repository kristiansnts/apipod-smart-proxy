package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Executor struct {
	logger     *log.Logger
	bgShells   map[string]*bgShell
	bgMu       sync.Mutex
	todos      []TodoItem
}

type bgShell struct {
	cmd    *exec.Cmd
	output strings.Builder
	mu     sync.Mutex
}

type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

func NewExecutor(logger *log.Logger) *Executor {
	return &Executor{
		logger:   logger,
		bgShells: make(map[string]*bgShell),
	}
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
	case "MultiEdit":
		result = e.executeMultiEdit(call)
	case "Glob":
		result = e.executeGlob(call)
	case "Grep":
		result = e.executeGrep(call)
	case "BashOutput":
		result = e.executeBashOutput(call)
	case "KillBash":
		result = e.executeKillBash(call)
	case "Task":
		result = e.executeTask(call)
	case "TodoWrite":
		result = e.executeTodoWrite(call)
	case "WebFetch":
		result = e.executeWebFetch(call)
	case "WebSearch":
		result = e.executeWebSearch(call)
	case "NotebookEdit":
		result = e.executeNotebookEdit(call)
	case "ExitPlanMode":
		result = e.executeExitPlanMode(call)
	case "LS":
		result = e.executeGlob(ToolCall{ID: call.ID, Name: "Glob", Input: map[string]interface{}{"pattern": "*"}})
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
	
	args := []string{pattern}
	if path, ok := call.Input["path"].(string); ok && path != "" {
		args = append(args, path)
	} else {
		args = append(args, ".")
	}
	
	cmd := exec.Command("rg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) == 0 {
			return ToolResult{
				ToolUseID: call.ID,
				Content:   "No matches found",
			}
		}
		return ToolResult{
			ToolUseID: call.ID,
			Content:   string(output),
		}
	}
	
	return ToolResult{
		ToolUseID: call.ID,
		Content:   string(output),
	}
}

func (e *Executor) executeMultiEdit(call ToolCall) ToolResult {
	filePath, _ := call.Input["file_path"].(string)
	if filePath == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: file_path", IsError: true}
	}

	editsRaw, ok := call.Input["edits"].([]interface{})
	if !ok || len(editsRaw) == 0 {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: edits", IsError: true}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}
	}

	text := string(content)
	for i, editRaw := range editsRaw {
		edit, ok := editRaw.(map[string]interface{})
		if !ok {
			return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Invalid edit at index %d", i), IsError: true}
		}
		oldStr, _ := edit["old_string"].(string)
		newStr, _ := edit["new_string"].(string)
		replaceAll, _ := edit["replace_all"].(bool)

		if oldStr == "" {
			return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Empty old_string at edit %d", i), IsError: true}
		}
		if !strings.Contains(text, oldStr) {
			return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("String not found at edit %d", i), IsError: true}
		}
		if replaceAll {
			text = strings.ReplaceAll(text, oldStr, newStr)
		} else {
			text = strings.Replace(text, oldStr, newStr, 1)
		}
	}

	if err := os.WriteFile(filePath, []byte(text), 0644); err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Error writing file: %v", err), IsError: true}
	}

	return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Applied %d edits to %s", len(editsRaw), filePath)}
}

func (e *Executor) executeBashOutput(call ToolCall) ToolResult {
	bashID, _ := call.Input["bash_id"].(string)
	if bashID == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: bash_id", IsError: true}
	}

	e.bgMu.Lock()
	shell, exists := e.bgShells[bashID]
	e.bgMu.Unlock()

	if !exists {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("No background shell with id: %s", bashID), IsError: true}
	}

	shell.mu.Lock()
	output := shell.output.String()
	shell.output.Reset()
	shell.mu.Unlock()

	if filterStr, ok := call.Input["filter"].(string); ok && filterStr != "" {
		re, err := regexp.Compile(filterStr)
		if err != nil {
			return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Invalid filter regex: %v", err), IsError: true}
		}
		var filtered []string
		for _, line := range strings.Split(output, "\n") {
			if re.MatchString(line) {
				filtered = append(filtered, line)
			}
		}
		output = strings.Join(filtered, "\n")
	}

	if output == "" {
		output = "(no new output)"
	}

	return ToolResult{ToolUseID: call.ID, Content: output}
}

func (e *Executor) executeKillBash(call ToolCall) ToolResult {
	shellID, _ := call.Input["shell_id"].(string)
	if shellID == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: shell_id", IsError: true}
	}

	e.bgMu.Lock()
	shell, exists := e.bgShells[shellID]
	if exists {
		delete(e.bgShells, shellID)
	}
	e.bgMu.Unlock()

	if !exists {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("No background shell with id: %s", shellID), IsError: true}
	}

	if shell.cmd.Process != nil {
		shell.cmd.Process.Kill()
	}

	return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Shell %s terminated", shellID)}
}

func (e *Executor) executeTask(call ToolCall) ToolResult {
	description, _ := call.Input["description"].(string)
	prompt, _ := call.Input["prompt"].(string)
	subagentType, _ := call.Input["subagent_type"].(string)

	if prompt == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: prompt", IsError: true}
	}

	// Execute the task prompt as a bash command if it looks like one,
	// otherwise treat it as a description and acknowledge it
	e.logger.Printf("[tools/Task] type=%s desc=%s", subagentType, description)
	return ToolResult{
		ToolUseID: call.ID,
		Content:   fmt.Sprintf("Task acknowledged: [%s] %s\nPrompt: %s\nNote: Sub-agent delegation is handled at the proxy level. Use Bash or other tools directly for execution.", subagentType, description, prompt),
	}
}

func (e *Executor) executeTodoWrite(call ToolCall) ToolResult {
	todosRaw, ok := call.Input["todos"].([]interface{})
	if !ok {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: todos", IsError: true}
	}

	e.todos = nil
	for _, raw := range todosRaw {
		itemMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		e.todos = append(e.todos, TodoItem{
			Content:    fmt.Sprintf("%v", itemMap["content"]),
			Status:     fmt.Sprintf("%v", itemMap["status"]),
			ActiveForm: fmt.Sprintf("%v", itemMap["activeForm"]),
		})
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Todo list updated (%d items):\n", len(e.todos)))
	for i, t := range e.todos {
		icon := "⬜"
		switch t.Status {
		case "in_progress":
			icon = "🔄"
		case "completed":
			icon = "✅"
		}
		sb.WriteString(fmt.Sprintf("  %s %d. %s [%s]\n", icon, i+1, t.Content, t.Status))
	}

	return ToolResult{ToolUseID: call.ID, Content: sb.String()}
}

func (e *Executor) executeWebFetch(call ToolCall) ToolResult {
	url, _ := call.Input["url"].(string)
	if url == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: url", IsError: true}
	}

	cmd := exec.Command("curl", "-sL", "--max-time", "15", "-H", "User-Agent: Mozilla/5.0", url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Fetch failed: %v", err), IsError: true}
	}

	content := string(output)
	// Truncate to 50KB to avoid overwhelming the model
	if len(content) > 50000 {
		content = content[:50000] + "\n...(truncated)"
	}

	return ToolResult{ToolUseID: call.ID, Content: content}
}

func (e *Executor) executeWebSearch(call ToolCall) ToolResult {
	query, _ := call.Input["query"].(string)
	if query == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: query", IsError: true}
	}

	// Use DuckDuckGo HTML lite as a simple search
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", strings.ReplaceAll(query, " ", "+"))
	cmd := exec.Command("curl", "-sL", "--max-time", "10", "-H", "User-Agent: Mozilla/5.0", searchURL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Search failed: %v", err), IsError: true}
	}

	// Extract result snippets from DuckDuckGo HTML
	content := string(output)
	var results []string
	// Simple extraction of result links and snippets
	parts := strings.Split(content, "result__a")
	for i, part := range parts {
		if i == 0 || i > 10 {
			continue
		}
		// Extract href
		if hrefIdx := strings.Index(part, "href=\""); hrefIdx != -1 {
			end := strings.Index(part[hrefIdx+6:], "\"")
			if end != -1 {
				href := part[hrefIdx+6 : hrefIdx+6+end]
				// Extract visible text
				if gtIdx := strings.Index(part, ">"); gtIdx != -1 {
					ltIdx := strings.Index(part[gtIdx:], "<")
					if ltIdx != -1 {
						title := strings.TrimSpace(part[gtIdx+1 : gtIdx+ltIdx])
						title = strings.ReplaceAll(title, "<b>", "")
						title = strings.ReplaceAll(title, "</b>", "")
						if title != "" && href != "" {
							results = append(results, fmt.Sprintf("- %s\n  %s", title, href))
						}
					}
				}
			}
		}
	}

	if len(results) == 0 {
		return ToolResult{ToolUseID: call.ID, Content: "No search results found"}
	}

	return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Search results for '%s':\n%s", query, strings.Join(results, "\n"))}
}

func (e *Executor) executeNotebookEdit(call ToolCall) ToolResult {
	notebookPath, _ := call.Input["notebook_path"].(string)
	if notebookPath == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: notebook_path", IsError: true}
	}

	content, err := os.ReadFile(notebookPath)
	if err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Error reading notebook: %v", err), IsError: true}
	}

	var notebook map[string]interface{}
	if err := json.Unmarshal(content, &notebook); err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Invalid notebook format: %v", err), IsError: true}
	}

	newSource, _ := call.Input["new_source"].(string)
	editMode, _ := call.Input["edit_mode"].(string)
	if editMode == "" {
		editMode = "replace"
	}
	cellID, _ := call.Input["cell_id"].(string)
	cellType, _ := call.Input["cell_type"].(string)

	cells, _ := notebook["cells"].([]interface{})

	switch editMode {
	case "replace":
		if cellID == "" {
			return ToolResult{ToolUseID: call.ID, Content: "cell_id required for replace mode", IsError: true}
		}
		found := false
		for _, cell := range cells {
			cellMap, ok := cell.(map[string]interface{})
			if !ok {
				continue
			}
			meta, _ := cellMap["metadata"].(map[string]interface{})
			id, _ := meta["id"].(string)
			if id == "" {
				id, _ = cellMap["id"].(string)
			}
			if id == cellID {
				cellMap["source"] = strings.Split(newSource, "\n")
				if cellType != "" {
					cellMap["cell_type"] = cellType
				}
				found = true
				break
			}
		}
		if !found {
			return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Cell %s not found", cellID), IsError: true}
		}

	case "insert":
		if cellType == "" {
			cellType = "code"
		}
		newCell := map[string]interface{}{
			"cell_type": cellType,
			"source":    strings.Split(newSource, "\n"),
			"metadata":  map[string]interface{}{},
			"outputs":   []interface{}{},
		}
		if cellID == "" {
			cells = append([]interface{}{newCell}, cells...)
		} else {
			inserted := false
			var newCells []interface{}
			for _, cell := range cells {
				newCells = append(newCells, cell)
				cellMap, ok := cell.(map[string]interface{})
				if ok {
					meta, _ := cellMap["metadata"].(map[string]interface{})
					id, _ := meta["id"].(string)
					if id == "" {
						id, _ = cellMap["id"].(string)
					}
					if id == cellID {
						newCells = append(newCells, newCell)
						inserted = true
					}
				}
			}
			if !inserted {
				newCells = append(newCells, newCell)
			}
			cells = newCells
		}

	case "delete":
		if cellID == "" {
			return ToolResult{ToolUseID: call.ID, Content: "cell_id required for delete mode", IsError: true}
		}
		var newCells []interface{}
		for _, cell := range cells {
			cellMap, ok := cell.(map[string]interface{})
			if !ok {
				newCells = append(newCells, cell)
				continue
			}
			meta, _ := cellMap["metadata"].(map[string]interface{})
			id, _ := meta["id"].(string)
			if id == "" {
				id, _ = cellMap["id"].(string)
			}
			if id != cellID {
				newCells = append(newCells, cell)
			}
		}
		cells = newCells
	}

	notebook["cells"] = cells
	out, err := json.MarshalIndent(notebook, "", " ")
	if err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Error serializing notebook: %v", err), IsError: true}
	}

	if err := os.WriteFile(notebookPath, out, 0644); err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Error writing notebook: %v", err), IsError: true}
	}

	return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("Notebook %s: %s operation completed", notebookPath, editMode)}
}

func (e *Executor) executeExitPlanMode(call ToolCall) ToolResult {
	plan, _ := call.Input["plan"].(string)
	if plan == "" {
		return ToolResult{ToolUseID: call.ID, Content: "Missing required parameter: plan", IsError: true}
	}

	return ToolResult{
		ToolUseID: call.ID,
		Content:   fmt.Sprintf("Plan submitted for approval:\n\n%s", plan),
	}
}