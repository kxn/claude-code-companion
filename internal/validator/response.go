package validator

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ResponseValidator struct {
	// 注释：strict_mode 和 validate_stream 已永久启用
}

func NewResponseValidator() *ResponseValidator {
	return &ResponseValidator{}
}

func (v *ResponseValidator) ValidateAnthropicResponse(body []byte, isStreaming bool) error {
	return v.ValidateResponse(body, isStreaming, "anthropic", "")
}

func (v *ResponseValidator) ValidateResponse(body []byte, isStreaming bool, endpointType, endpointURL string) error {
	return v.ValidateResponseWithPath(body, isStreaming, endpointType, "", endpointURL)
}

func (v *ResponseValidator) ValidateResponseWithPath(body []byte, isStreaming bool, endpointType, path, endpointURL string) error {
	// 流式验证和严格模式已永久启用

	// 跳过 count_tokens 接口的 Anthropic 格式验证
	if isCountTokensEndpoint(path) {
		// count_tokens 接口只做基本 JSON 格式验证
		var response map[string]interface{}
		if err := json.Unmarshal(body, &response); err != nil {
			return fmt.Errorf("invalid JSON response: %v", err)
		}
		// count_tokens 应该返回包含 input_tokens 的响应
		if _, hasInputTokens := response["input_tokens"]; hasInputTokens {
			return nil
		}
		return fmt.Errorf("count_tokens response missing input_tokens field")
	}

	if isStreaming {
		// 首先进行基本的SSE chunk验证
		if err := v.ValidateSSEChunk(body, endpointType); err != nil {
			return err
		}
		// 然后验证完整SSE流的完整性
		return v.ValidateCompleteSSEStream(body, endpointType, endpointURL)
	}
	return v.ValidateStandardResponse(body, endpointType)
}

// isCountTokensEndpoint 检查是否为 count_tokens 接口
func isCountTokensEndpoint(path string) bool {
	return strings.Contains(path, "/count_tokens")
}

func (v *ResponseValidator) ValidateStandardResponse(body []byte, endpointType string) error {
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("invalid JSON response: %v", err)
	}

	// 严格模式已永久启用
	if endpointType == "anthropic" {
		requiredFields := []string{"id", "type", "content", "model"}
		for _, field := range requiredFields {
			if _, exists := response[field]; !exists {
				return fmt.Errorf("missing required field: %s", field)
			}
		}

		if msgType, ok := response["type"].(string); !ok || msgType != "message" {
			return fmt.Errorf("invalid message type: expected 'message', got '%v'", response["type"])
		}

		if role, exists := response["role"]; exists {
			if roleStr, ok := role.(string); !ok || roleStr != "assistant" {
				return fmt.Errorf("invalid role: expected 'assistant', got '%v'", role)
			}
		}
	} else if endpointType == "openai" {
		// OpenAI格式验证：检查基本结构
		requiredFields := []string{"id", "model"}
		for _, field := range requiredFields {
			if _, exists := response[field]; !exists {
				return fmt.Errorf("missing required field for OpenAI format: %s", field)
			}
		}

		// 验证是否有choices或error字段
		_, hasChoices := response["choices"]
		_, hasError := response["error"]
		if !hasChoices && !hasError {
			return fmt.Errorf("OpenAI response missing both 'choices' and 'error' fields")
		}

		// 如果有object字段，验证其值（可选）
		if objectType, ok := response["object"].(string); ok {
			if objectType != "chat.completion" && objectType != "chat.completion.chunk" {
				return fmt.Errorf("invalid object type for OpenAI: expected 'chat.completion' or 'chat.completion.chunk', got '%v'", objectType)
			}
		}
	} else {
		// 非严格模式：只要是有效JSON且包含content或error字段之一即可
		if _, hasContent := response["content"]; hasContent {
			return nil
		}
		if _, hasError := response["error"]; hasError {
			return nil
		}
		if _, hasChoices := response["choices"]; hasChoices {
			return nil // OpenAI格式通常有choices字段
		}
		// 如果既没有content也没有error也没有choices，认为是无效响应
		return fmt.Errorf("response missing both 'content', 'error' and 'choices' fields")
	}

	return nil
}

func (v *ResponseValidator) ValidateSSEChunk(chunk []byte, endpointType string) error {
	lines := bytes.Split(chunk, []byte("\n"))

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if bytes.HasPrefix(line, []byte("event: ")) {
			eventType := string(line[7:])

			if endpointType == "anthropic" {
				validEvents := []string{
					"message_start", "content_block_start", "ping",
					"content_block_delta", "content_block_stop", "message_stop",
					"message_delta", "error",
				}

				valid := false
				for _, validEvent := range validEvents {
					if eventType == validEvent {
						valid = true
						break
					}
				}

				if !valid {
					return fmt.Errorf("invalid SSE event type for Anthropic: %s", eventType)
				}
			}
			// OpenAI格式通常不使用event字段，或者使用不同的事件类型，这里不做严格验证
		}

		if bytes.HasPrefix(line, []byte("data: ")) {
			dataContent := line[6:]
			if len(dataContent) == 0 || string(dataContent) == "[DONE]" {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal(dataContent, &data); err != nil {
				return fmt.Errorf("invalid JSON in SSE data: %v", err)
			}

			// 严格模式已永久启用
			if endpointType == "anthropic" {
				if _, hasType := data["type"]; !hasType {
					return fmt.Errorf("missing 'type' field in SSE data")
				}

				// 检查message_start事件的usage统计
				if err := v.ValidateMessageStartUsage(data); err != nil {
					return err
				}
			} else if endpointType == "openai" {
				// OpenAI格式验证：检查基本字段
				if _, hasId := data["id"]; !hasId {
					return fmt.Errorf("missing 'id' field in OpenAI SSE data")
				}
				if _, hasModel := data["model"]; !hasModel {
					return fmt.Errorf("missing 'model' field in OpenAI SSE data")
				}
				// OpenAI格式不要求type和object字段
			}
		}
	}

	return nil
}

// ValidateCompleteSSEStream 验证完整的SSE流是否包含所有必需的事件
func (v *ResponseValidator) ValidateCompleteSSEStream(body []byte, endpointType, endpointURL string) error {
	if endpointType == "anthropic" {
		return v.validateAnthropicSSECompleteness(body)
	} else if endpointType == "openai" {
		return v.validateOpenAISSECompleteness(body, endpointURL)
	}
	return nil
}

// validateAnthropicSSECompleteness 验证Anthropic SSE流的完整性
func (v *ResponseValidator) validateAnthropicSSECompleteness(body []byte) error {
	lines := bytes.Split(body, []byte("\n"))
	hasMessageStart := false
	hasMessageStop := false

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("event: ")) {
			eventType := string(line[7:])
			switch eventType {
			case "message_start":
				hasMessageStart = true
			case "message_stop":
				hasMessageStop = true
			}
		}
	}

	if hasMessageStart && !hasMessageStop {
		return fmt.Errorf("incomplete SSE stream: has message_start but missing message_stop event")
	}

	return nil
}

// validateOpenAISSECompleteness 验证OpenAI SSE流的完整性
func (v *ResponseValidator) validateOpenAISSECompleteness(body []byte, endpointURL string) error {
	bodyStr := string(body)

	// OpenAI流式响应应该以[DONE]结束
	if !strings.Contains(bodyStr, "[DONE]") {
		if !strings.Contains(endpointURL, "apis.iflow.cn") {
			return fmt.Errorf("incomplete OpenAI SSE stream: missing [DONE] marker")
		}
	}

	// 检查是否有finish_reason
	lines := bytes.Split(body, []byte("\n"))
	hasFinishReason := false

	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("data: ")) {
			dataContent := line[6:]
			if len(dataContent) == 0 || string(dataContent) == "[DONE]" {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal(dataContent, &data); err != nil {
				continue
			}

			if choices, ok := data["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if finishReason, exists := choice["finish_reason"]; exists && finishReason != nil {
						hasFinishReason = true
						break
					}
				}
			}
		}
	}

	if !hasFinishReason {
		return fmt.Errorf("incomplete OpenAI SSE stream: missing finish_reason in response")
	}

	return nil
}

func (v *ResponseValidator) DecompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress gzip data: %v", err)
	}

	return decompressed, nil
}

func (v *ResponseValidator) GetDecompressedBody(body []byte, contentEncoding string) ([]byte, error) {
	if strings.Contains(strings.ToLower(contentEncoding), "gzip") {
		return v.DecompressGzip(body)
	}
	return body, nil
}

func (v *ResponseValidator) IsGzipContent(contentEncoding string) bool {
	return strings.Contains(strings.ToLower(contentEncoding), "gzip")
}

func (v *ResponseValidator) ValidateMessageStartUsage(eventData map[string]interface{}) error {
	eventType, ok := eventData["type"].(string)
	if !ok || eventType != "message_start" {
		return nil
	}

	message, ok := eventData["message"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message_start: missing message field")
	}

	usage, ok := message["usage"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message_start: missing usage field")
	}

	// 检查是否存在 input_tokens 和 output_tokens 字段
	_, hasInputTokens := usage["input_tokens"]
	_, hasOutputTokens := usage["output_tokens"]

	if hasInputTokens && hasOutputTokens {
		// 如果存在标准字段，直接认为是合法的（不管值是什么）
		return nil
	} else {
		// 如果不存在标准字段，检查是否为不合法的格式
		promptTokens := float64(-1)
		completionTokens := float64(-1)
		totalTokens := float64(-1)

		if val, ok := usage["prompt_tokens"]; ok {
			if num, ok := val.(float64); ok {
				promptTokens = num
			}
		}

		if val, ok := usage["completion_tokens"]; ok {
			if num, ok := val.(float64); ok {
				completionTokens = num
			}
		}

		if val, ok := usage["total_tokens"]; ok {
			if num, ok := val.(float64); ok {
				totalTokens = num
			}
		}

		// 只有当三个字段都存在且都为0时才判定为不合法
		if promptTokens == 0 && completionTokens == 0 && totalTokens == 0 {
			return fmt.Errorf("invalid usage stats: prompt_tokens, completion_tokens and total_tokens are all zero, indicating malformed response")
		}
	}

	return nil
}

// DetectJSONContent 检测内容是否为JSON格式（而非SSE格式）
func (v *ResponseValidator) DetectJSONContent(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	// 检查是否为有效JSON
	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		return false
	}

	// 如果能成功解析为JSON，就是JSON内容
	// 不需要检查内容中是否包含SSE相关字符串，因为JSON可以包含任何字符串内容
	return true
}

// DetectSSEContent 检测内容是否为SSE格式
func (v *ResponseValidator) DetectSSEContent(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	bodyStr := string(body)

	// 首先检查是否是有效的JSON，如果是JSON则不是SSE
	trimmed := strings.TrimSpace(bodyStr)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		// 尝试解析JSON来确认
		var temp interface{}
		if json.Unmarshal(body, &temp) == nil {
			return false // 是有效的JSON，不是SSE
		}
	}

	// 检查SSE格式：必须有以"event: "或"data: "开头的行
	lines := strings.Split(bodyStr, "\n")
	hasEventLine := false
	hasDataLine := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "event: ") {
			hasEventLine = true
		}
		if strings.HasPrefix(trimmedLine, "data: ") {
			hasDataLine = true
		}
	}

	// SSE格式必须至少有一个data:行，通常还有event:行
	return hasDataLine && (hasEventLine || len(lines) > 1)
}

// SmartDetectContentType 智能检测内容类型并返回应该设置的Content-Type和覆盖信息
// 返回值: (newContentType, overrideInfo)
// - newContentType: 应该设置的Content-Type，空字符串表示不需要修改
// - overrideInfo: 覆盖信息，用于日志记录，格式如 "json->sse" 或 "sse->json"
func (v *ResponseValidator) SmartDetectContentType(body []byte, currentContentType string, statusCode int) (string, string) {
	if statusCode != 200 || len(body) == 0 {
		return "", "" // 只处理200状态码的响应
	}

	// 标准化当前Content-Type
	currentContentTypeLower := strings.ToLower(currentContentType)
	isCurrentSSE := strings.Contains(currentContentTypeLower, "text/event-stream")
	isCurrentJSON := strings.Contains(currentContentTypeLower, "application/json")
	isCurrentPlain := strings.Contains(currentContentTypeLower, "text/plain")

	// 检测实际内容类型
	isActualSSE := v.DetectSSEContent(body)
	isActualJSON := v.DetectJSONContent(body)

	// 决定是否需要覆盖Content-Type
	if isActualSSE && !isCurrentSSE {
		// 内容是SSE但Content-Type不是，覆盖为SSE
		if isCurrentJSON {
			return "text/event-stream; charset=utf-8", "json->sse"
		} else if isCurrentPlain {
			return "text/event-stream; charset=utf-8", "plain->sse"
		} else {
			return "text/event-stream; charset=utf-8", "unknown->sse"
		}
	} else if isActualJSON && !isCurrentJSON {
		// 内容是JSON但Content-Type不是，覆盖为JSON
		if isCurrentSSE {
			return "application/json", "sse->json"
		} else if isCurrentPlain {
			return "application/json", "plain->json"
		} else {
			return "application/json", "unknown->json"
		}
	}

	// 不需要覆盖
	return "", ""
}
