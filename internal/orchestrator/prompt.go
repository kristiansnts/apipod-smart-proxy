package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type ToolGroup struct {
	Tools         []string `json:"tools"`
	PromptSections []string `json:"prompt_sections"`
}

func LoadPromptSections(sections []string) (string, error) {
	var parts []string
	for _, section := range sections {
		data, err := os.ReadFile(filepath.Join("system_prompt", section+".txt"))
		if err != nil {
			continue
		}
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	return strings.Join(parts, "\n\n"), nil
}

func LoadFullPrompt() (string, error) {
	data, err := os.ReadFile("system_prompt.txt")
	if err != nil {
		return LoadPromptSections([]string{"core", "code_conventions", "task_management", "tool_efficiency", "tool_policy", "doing_tasks"})
	}
	return string(data), nil
}

func LoadToolGroups() (map[string]ToolGroup, error) {
	data, err := os.ReadFile("tools/tool_groups.json")
	if err != nil {
		return nil, err
	}
	var groups map[string]ToolGroup
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func LoadAllTools() ([]interface{}, error) {
	entries, err := os.ReadDir("tools/mcp")
	if err != nil {
		return nil, err
	}

	var tools []interface{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("tools/mcp", entry.Name()))
		if err != nil {
			continue
		}
		var tool interface{}
		if err := json.Unmarshal(data, &tool); err != nil {
			continue
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func LoadToolsByNames(names []string) ([]interface{}, error) {
	for _, name := range names {
		if name == "*" {
			return LoadAllTools()
		}
	}

	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	allTools, err := LoadAllTools()
	if err != nil {
		return nil, err
	}

	var filtered []interface{}
	for _, tool := range allTools {
		if toolMap, ok := tool.(map[string]interface{}); ok {
			if name, ok := toolMap["name"].(string); ok && nameSet[name] {
				filtered = append(filtered, tool)
			}
		}
	}
	return filtered, nil
}

func GetGroupForIntent(intent string) (*ToolGroup, error) {
	groups, err := LoadToolGroups()
	if err != nil {
		return nil, err
	}
	group, ok := groups[intent]
	if !ok {
		fullGroup := groups["full"]
		return &fullGroup, nil
	}
	return &group, nil
}
