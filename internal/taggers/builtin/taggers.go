package builtin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"claude-code-companion/internal/interfaces"
)

// wildcardMatch 统一的通配符匹配函数，支持更直观的通配符语义
// * 匹配任意字符序列
// ? 匹配单个字符
func wildcardMatch(pattern, str string) (bool, error) {
	// 将通配符模式转换为正则表达式
	regexPattern := wildcardToRegex(pattern)
	
	// 编译正则表达式
	regex, err := regexp.Compile("^" + regexPattern + "$")
	if err != nil {
		return false, fmt.Errorf("invalid pattern '%s': %v", pattern, err)
	}
	
	return regex.MatchString(str), nil
}

// wildcardToRegex 将通配符模式转换为正则表达式
func wildcardToRegex(pattern string) string {
	// 转义正则表达式特殊字符，但保留我们的通配符
	escaped := regexp.QuoteMeta(pattern)
	
	// 将转义后的通配符还原并转换为正则表达式
	// \* -> .* (匹配任意字符序列)
	// \? -> . (匹配单个字符)
	escaped = strings.ReplaceAll(escaped, `\*`, `.*`)
	escaped = strings.ReplaceAll(escaped, `\?`, `.`)
	
	return escaped
}

// BaseTagger 内置tagger的基础结构
type BaseTagger struct {
	name string
	tag  string
}

func (bt *BaseTagger) Name() string { return bt.name }
func (bt *BaseTagger) Tag() string  { return bt.tag }

// PathTagger 路径匹配tagger
type PathTagger struct {
	BaseTagger
	pathPattern string
}

// NewPathTagger 创建路径匹配tagger
func NewPathTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	pathPattern, ok := config["path_pattern"].(string)
	if !ok || pathPattern == "" {
		return nil, fmt.Errorf("path tagger requires 'path_pattern' in config")
	}

	return &PathTagger{
		BaseTagger:  BaseTagger{name: name, tag: tag},
		pathPattern: pathPattern,
	}, nil
}

func (pt *PathTagger) ShouldTag(request *http.Request) (bool, error) {
	// 使用统一的通配符匹配函数
	return wildcardMatch(pt.pathPattern, request.URL.Path)
}

// HeaderTagger 请求头匹配tagger
type HeaderTagger struct {
	BaseTagger
	headerName    string
	expectedValue string
}

// NewHeaderTagger 创建请求头匹配tagger
func NewHeaderTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	headerName, ok := config["header_name"].(string)
	if !ok || headerName == "" {
		return nil, fmt.Errorf("header tagger requires 'header_name' in config")
	}

	expectedValue, ok := config["expected_value"].(string)
	if !ok || expectedValue == "" {
		return nil, fmt.Errorf("header tagger requires 'expected_value' in config")
	}

	return &HeaderTagger{
		BaseTagger:    BaseTagger{name: name, tag: tag},
		headerName:    headerName,
		expectedValue: expectedValue,
	}, nil
}

func (ht *HeaderTagger) ShouldTag(request *http.Request) (bool, error) {
	headerValue := request.Header.Get(ht.headerName)
	if headerValue == "" {
		return false, nil
	}

	// 使用统一的通配符匹配函数
	return wildcardMatch(ht.expectedValue, headerValue)
}

// ModelTagger 模型匹配tagger (专门匹配请求体中的model字段)
type ModelTagger struct {
	BaseTagger
	expectedValue string
}

// NewModelTagger 创建模型匹配tagger
func NewModelTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	expectedValue, ok := config["expected_value"].(string)
	if !ok || expectedValue == "" {
		return nil, fmt.Errorf("model tagger requires 'expected_value' in config")
	}

	return &ModelTagger{
		BaseTagger:    BaseTagger{name: name, tag: tag},
		expectedValue: expectedValue,
	}, nil
}

func (mt *ModelTagger) ShouldTag(request *http.Request) (bool, error) {
	// 只处理JSON内容类型
	contentType := request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return false, nil
	}

	// 从请求上下文中获取预处理的请求体数据
	bodyContent, ok := request.Context().Value("cached_body").([]byte)
	if !ok || len(bodyContent) == 0 {
		return false, nil
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(bodyContent, &jsonData); err != nil {
		return false, nil // JSON解析失败，不匹配
	}

	// 提取model字段
	modelValue, ok := jsonData["model"]
	if !ok {
		return false, nil
	}

	if strValue, ok := modelValue.(string); ok {
		// 使用统一的通配符匹配函数
		return wildcardMatch(mt.expectedValue, strValue)
	}

	return false, nil
}

// QueryTagger 查询参数匹配tagger
type QueryTagger struct {
	BaseTagger
	paramName     string
	expectedValue string
}

// NewQueryTagger 创建查询参数匹配tagger
func NewQueryTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	paramName, ok := config["param_name"].(string)
	if !ok || paramName == "" {
		return nil, fmt.Errorf("query tagger requires 'param_name' in config")
	}

	expectedValue, ok := config["expected_value"].(string)
	if !ok || expectedValue == "" {
		return nil, fmt.Errorf("query tagger requires 'expected_value' in config")
	}

	return &QueryTagger{
		BaseTagger:    BaseTagger{name: name, tag: tag},
		paramName:     paramName,
		expectedValue: expectedValue,
	}, nil
}

func (qt *QueryTagger) ShouldTag(request *http.Request) (bool, error) {
	paramValue := request.URL.Query().Get(qt.paramName)
	if paramValue == "" {
		return false, nil
	}

	// 使用统一的通配符匹配函数
	return wildcardMatch(qt.expectedValue, paramValue)
}

// BodyJSONTagger JSON请求体字段匹配tagger
type BodyJSONTagger struct {
	BaseTagger
	jsonPath      string
	expectedValue string
}

// NewBodyJSONTagger 创建JSON请求体字段匹配tagger
func NewBodyJSONTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	jsonPath, ok := config["json_path"].(string)
	if !ok || jsonPath == "" {
		return nil, fmt.Errorf("body-json tagger requires 'json_path' in config")
	}

	expectedValue, ok := config["expected_value"].(string)
	if !ok || expectedValue == "" {
		return nil, fmt.Errorf("body-json tagger requires 'expected_value' in config")
	}

	return &BodyJSONTagger{
		BaseTagger:    BaseTagger{name: name, tag: tag},
		jsonPath:      jsonPath,
		expectedValue: expectedValue,
	}, nil
}

func (bt *BodyJSONTagger) ShouldTag(request *http.Request) (bool, error) {
	// 只处理JSON内容类型
	contentType := request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return false, nil
	}

	// 从请求上下文中获取预处理的请求体数据
	// 这需要在调用tagger之前由pipeline预处理并设置到context中
	bodyContent, ok := request.Context().Value("cached_body").([]byte)
	if !ok || len(bodyContent) == 0 {
		return false, nil
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(bodyContent, &jsonData); err != nil {
		return false, nil // JSON解析失败，不匹配
	}

	// 简单的JSON路径解析（支持如 "model" 或 "data.model" 格式）
	value, err := bt.extractJSONValue(jsonData, bt.jsonPath)
	if err != nil {
		return false, nil
	}

	if strValue, ok := value.(string); ok {
		// 使用统一的通配符匹配函数
		return wildcardMatch(bt.expectedValue, strValue)
	}

	return false, nil
}

// extractJSONValue 从JSON数据中提取指定路径的值
func (bt *BodyJSONTagger) extractJSONValue(data map[string]interface{}, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			// 最后一个部分，返回值
			return current[part], nil
		}

		// 中间部分，继续深入
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			return nil, fmt.Errorf("invalid path: %s", path)
		}
	}

	return nil, fmt.Errorf("empty path")
}

// UserMessageTagger 用户最新消息内容匹配tagger
// 匹配 messages 中最后一条 role 为 user 的消息的最后一个 text 类型内容
type UserMessageTagger struct {
	BaseTagger
	expectedValue string
}

// NewUserMessageTagger 创建用户消息内容匹配tagger
func NewUserMessageTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	expectedValue, ok := config["expected_value"].(string)
	if !ok || expectedValue == "" {
		return nil, fmt.Errorf("user-message tagger requires 'expected_value' in config")
	}

	return &UserMessageTagger{
		BaseTagger:    BaseTagger{name: name, tag: tag},
		expectedValue: expectedValue,
	}, nil
}

func (ut *UserMessageTagger) ShouldTag(request *http.Request) (bool, error) {
	// 只处理JSON内容类型
	contentType := request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return false, nil
	}

	// 从请求上下文中获取预处理的请求体数据
	bodyContent, ok := request.Context().Value("cached_body").([]byte)
	if !ok || len(bodyContent) == 0 {
		return false, nil
	}

	var requestData map[string]interface{}
	if err := json.Unmarshal(bodyContent, &requestData); err != nil {
		return false, nil // JSON解析失败，不匹配
	}

	// 提取用户最新消息的文本内容
	userText, err := ut.extractLatestUserMessage(requestData)
	if err != nil {
		return false, nil
	}

	if userText == "" {
		return false, nil
	}

	// 使用统一的通配符匹配函数
	return wildcardMatch(ut.expectedValue, userText)
}

// extractLatestUserMessage 提取用户最新消息的文本内容
// 从 messages 中找到最后一条 role 为 "user" 的消息，取其 content 中最后一个 text 类型的 text 字段
func (ut *UserMessageTagger) extractLatestUserMessage(data map[string]interface{}) (string, error) {
	// 获取 messages 数组
	messagesInterface, ok := data["messages"]
	if !ok {
		return "", fmt.Errorf("no messages field found")
	}

	messages, ok := messagesInterface.([]interface{})
	if !ok {
		return "", fmt.Errorf("messages field is not an array")
	}

	// 从后往前遍历，找到最后一条 role 为 "user" 的消息
	for i := len(messages) - 1; i >= 0; i-- {
		msgInterface := messages[i]
		msg, ok := msgInterface.(map[string]interface{})
		if !ok {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok || role != "user" {
			continue
		}

		// 找到了最后一条用户消息，提取 content
		contentInterface, ok := msg["content"]
		if !ok {
			continue
		}

		// content 可能是字符串或数组
		switch content := contentInterface.(type) {
		case string:
			// 简单字符串格式
			return content, nil

		case []interface{}:
			// 数组格式，找最后一个 text 类型的内容
			var lastText string
			for _, itemInterface := range content {
				item, ok := itemInterface.(map[string]interface{})
				if !ok {
					continue
				}

				itemType, ok := item["type"].(string)
				if !ok || itemType != "text" {
					continue
				}

				text, ok := item["text"].(string)
				if ok {
					lastText = text // 保存最后一个 text
				}
			}

			if lastText != "" {
				return lastText, nil
			}
		}

		// 如果找到了用户消息但没有有效的text内容，继续找前一条用户消息
		// 但这里我们只找最后一条，所以break
		break
	}

	return "", fmt.Errorf("no user message found")
}

// ThinkingTagger thinking模式匹配tagger
type ThinkingTagger struct {
	BaseTagger
	minBudgetTokens int
}

// NewThinkingTagger 创建thinking模式匹配tagger
func NewThinkingTagger(name, tag string, config map[string]interface{}) (interfaces.Tagger, error) {
	minBudgetTokens := 0 // 默认值为0
	
	if budgetInterface, ok := config["min_budget_tokens"]; ok {
		if budgetFloat, ok := budgetInterface.(float64); ok {
			minBudgetTokens = int(budgetFloat)
		} else if budgetInt, ok := budgetInterface.(int); ok {
			minBudgetTokens = budgetInt
		} else if budgetStr, ok := budgetInterface.(string); ok {
			if i, err := strconv.Atoi(budgetStr); err == nil {
				minBudgetTokens = i
			} else {
				return nil, fmt.Errorf("thinking tagger 'min_budget_tokens' must be a number")
			}
		} else {
			return nil, fmt.Errorf("thinking tagger 'min_budget_tokens' must be a number")
		}
	}

	if minBudgetTokens < 0 {
		return nil, fmt.Errorf("thinking tagger 'min_budget_tokens' must be non-negative")
	}

	return &ThinkingTagger{
		BaseTagger:      BaseTagger{name: name, tag: tag},
		minBudgetTokens: minBudgetTokens,
	}, nil
}

func (tt *ThinkingTagger) ShouldTag(request *http.Request) (bool, error) {
	// 只处理JSON内容类型
	contentType := request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return false, nil
	}

	// 从请求上下文中获取预处理的请求体数据
	bodyContent, ok := request.Context().Value("cached_body").([]byte)
	if !ok || len(bodyContent) == 0 {
		return false, nil
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(bodyContent, &jsonData); err != nil {
		return false, nil // JSON解析失败，不匹配
	}

	// 检查是否启用了thinking模式
	thinkingInterface, ok := jsonData["thinking"]
	if !ok {
		return false, nil
	}

	thinkingData, ok := thinkingInterface.(map[string]interface{})
	if !ok {
		return false, nil
	}

	// 检查enabled字段
	enabled, ok := thinkingData["enabled"]
	if !ok {
		return false, nil
	}

	enabledBool, ok := enabled.(bool)
	if !ok || !enabledBool {
		return false, nil
	}

	// 如果设置了最小budget_tokens要求，检查budget_tokens字段
	if tt.minBudgetTokens > 0 {
		budgetTokens, ok := thinkingData["budget_tokens"]
		if !ok {
			return false, nil
		}

		var budgetValue int
		switch v := budgetTokens.(type) {
		case float64:
			budgetValue = int(v)
		case int:
			budgetValue = v
		default:
			return false, nil
		}

		if budgetValue < tt.minBudgetTokens {
			return false, nil
		}
	}

	// thinking已启用且满足budget_tokens要求
	return true, nil
}