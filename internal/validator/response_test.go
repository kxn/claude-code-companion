package validator

import (
	"testing"
)

func TestValidateMessageStartUsage(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 正常的message_start事件，包含非零usage统计
	validEvent := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id": "msg_123",
			"usage": map[string]interface{}{
				"input_tokens":  float64(100),
				"output_tokens": float64(0), // output可以为0，因为刚开始
			},
		},
	}

	err := validator.ValidateMessageStartUsage(validEvent)
	if err != nil {
		t.Errorf("Expected valid event to pass, got error: %v", err)
	}

	// 测试用例2: 无效事件，使用旧版本字段且所有token统计都为0
	invalidEvent := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id": "msg_123",
			"usage": map[string]interface{}{
				"prompt_tokens":     float64(0),
				"completion_tokens": float64(0),
				"total_tokens":      float64(0),
			},
		},
	}

	err = validator.ValidateMessageStartUsage(invalidEvent)
	if err == nil {
		t.Error("Expected invalid event (all tokens zero) to fail validation")
	}
	if !contains(err.Error(), "invalid usage stats") {
		t.Errorf("Expected error message to contain 'invalid usage stats', got: %v", err)
	}

	// 测试用例3: 非message_start事件应该跳过验证
	otherEvent := map[string]interface{}{
		"type": "content_block_delta",
		"delta": map[string]interface{}{
			"text": "Hello",
		},
	}

	err = validator.ValidateMessageStartUsage(otherEvent)
	if err != nil {
		t.Errorf("Expected non-message_start event to be skipped, got error: %v", err)
	}
}

func TestValidateSSEChunkWithUsage(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 包含有效usage的流式响应
	validSSEData := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","usage":{"input_tokens":100,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0}

`)

	err := validator.ValidateSSEChunk(validSSEData, "anthropic")
	if err != nil {
		t.Errorf("Expected valid SSE data to pass, got error: %v", err)
	}

	// 测试用例2: 包含无效usage统计的流式响应（使用旧版本字段）
	invalidSSEData := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}}

`)

	err = validator.ValidateSSEChunk(invalidSSEData, "anthropic")
	if err == nil {
		t.Error("Expected invalid SSE data (zero tokens) to fail validation")
	}
	if !contains(err.Error(), "invalid usage stats") {
		t.Errorf("Expected error message to contain 'invalid usage stats', got: %v", err)
	}
}

func TestDetectJSONContent(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 纯JSON内容（非SSE格式）
	jsonContent := []byte(`{"id":"msg_123","type":"message","content":[{"type":"text","text":"Hello"}],"model":"claude-3"}`)

	if !validator.DetectJSONContent(jsonContent) {
		t.Error("Expected pure JSON content to be detected as JSON")
	}

	// 测试用例2: SSE格式内容
	sseContent := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123"}}

event: content_block_start  
data: {"type":"content_block_start","index":0}

`)

	if validator.DetectJSONContent(sseContent) {
		t.Error("Expected SSE content to NOT be detected as JSON")
	}

	// 测试用例3: 空内容
	emptyContent := []byte("")

	if validator.DetectJSONContent(emptyContent) {
		t.Error("Expected empty content to NOT be detected as JSON")
	}

	// 测试用例4: 无效JSON内容
	invalidContent := []byte("this is not json")

	if validator.DetectJSONContent(invalidContent) {
		t.Error("Expected invalid JSON content to NOT be detected as JSON")
	}

	// 测试用例5: JSON格式但包含SSE关键字（边界情况）
	// 如果能成功解析为JSON，就应该被识别为JSON，不管内容中包含什么关键字
	jsonWithSSEKeywords := []byte(`{"message":"This contains event: and data: keywords but is still JSON"}`)

	if !validator.DetectJSONContent(jsonWithSSEKeywords) {
		t.Error("Expected valid JSON to be detected as JSON, even if it contains SSE keywords in the content")
	}
}

func TestValidateCompleteSSEStream(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 完整的Anthropic SSE流
	completeAnthropicSSE := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123"}}

event: content_block_start
data: {"type":"content_block_start","index":0}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`)

	err := validator.ValidateCompleteSSEStream(completeAnthropicSSE, "anthropic", "")
	if err != nil {
		t.Errorf("Expected complete Anthropic SSE stream to pass validation, got error: %v", err)
	}

	// 测试用例2: 不完整的Anthropic SSE流（缺少message_stop）
	incompleteAnthropicSSE := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123"}}

event: content_block_start
data: {"type":"content_block_start","index":0}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"text":"Hello"}}

`)

	err = validator.ValidateCompleteSSEStream(incompleteAnthropicSSE, "anthropic", "")
	if err == nil {
		t.Error("Expected incomplete Anthropic SSE stream (missing message_stop) to fail validation")
	}
	if !contains(err.Error(), "incomplete SSE stream") {
		t.Errorf("Expected error message to contain 'incomplete SSE stream', got: %v", err)
	}

	// 测试用例3: 完整的OpenAI SSE流
	completeOpenAISSE := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`)

	err = validator.ValidateCompleteSSEStream(completeOpenAISSE, "openai", "https://api.openai.com")
	if err != nil {
		t.Errorf("Expected complete OpenAI SSE stream to pass validation, got error: %v", err)
	}

	// 测试用例4: 不完整的OpenAI SSE流（缺少[DONE]）
	incompleteOpenAISSE := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

`)

	err = validator.ValidateCompleteSSEStream(incompleteOpenAISSE, "openai", "https://api.openai.com")
	if err == nil {
		t.Error("Expected incomplete OpenAI SSE stream (missing [DONE]) to fail validation")
	}
	if !contains(err.Error(), "missing [DONE]") {
		t.Errorf("Expected error message to contain 'missing [DONE]', got: %v", err)
	}

	// 测试用例4b: 缺少[DONE]但域名为 apis.iflow.cn 应该通过
	err = validator.ValidateCompleteSSEStream(incompleteOpenAISSE, "openai", "https://apis.iflow.cn")
	if err != nil {
		t.Errorf("Expected OpenAI SSE without [DONE] from apis.iflow.cn to pass validation, got: %v", err)
	}

	// 测试用例5: OpenAI SSE流缺少finish_reason
	noFinishReasonSSE := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: [DONE]

`)

	err = validator.ValidateCompleteSSEStream(noFinishReasonSSE, "openai", "https://api.openai.com")
	if err == nil {
		t.Error("Expected OpenAI SSE stream without finish_reason to fail validation")
	}
	if !contains(err.Error(), "missing finish_reason") {
		t.Errorf("Expected error message to contain 'missing finish_reason', got: %v", err)
	}
}

func TestValidateAnthropicSSECompleteness(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 完整的Anthropic流（有message_start和message_stop）
	completeSSE := []byte(`event: message_start
data: {"type":"message_start"}

event: content_block_start
data: {"type":"content_block_start","index":0}

event: message_stop
data: {"type":"message_stop"}

`)

	err := validator.validateAnthropicSSECompleteness(completeSSE)
	if err != nil {
		t.Errorf("Expected complete Anthropic SSE to pass validation, got error: %v", err)
	}

	// 测试用例2: 不完整的流（有message_start但缺少message_stop）
	incompleteSSE := []byte(`event: message_start
data: {"type":"message_start"}

event: content_block_start
data: {"type":"content_block_start","index":0}

`)

	err = validator.validateAnthropicSSECompleteness(incompleteSSE)
	if err == nil {
		t.Error("Expected incomplete Anthropic SSE (missing message_stop) to fail validation")
	}
	if !contains(err.Error(), "missing message_stop") {
		t.Errorf("Expected error message to contain 'missing message_stop', got: %v", err)
	}

	// 测试用例3: 没有message_start的流（不应该报错）
	noMessageStartSSE := []byte(`event: content_block_start
data: {"type":"content_block_start","index":0}

event: content_block_delta
data: {"type":"content_block_delta","index":0}

`)

	err = validator.validateAnthropicSSECompleteness(noMessageStartSSE)
	if err != nil {
		t.Errorf("Expected SSE without message_start to pass validation, got error: %v", err)
	}

	// 测试用例4: 只有message_stop没有message_start（不应该报错）
	onlyMessageStopSSE := []byte(`event: content_block_delta
data: {"type":"content_block_delta","index":0}

event: message_stop
data: {"type":"message_stop"}

`)

	err = validator.validateAnthropicSSECompleteness(onlyMessageStopSSE)
	if err != nil {
		t.Errorf("Expected SSE with only message_stop to pass validation, got error: %v", err)
	}
}

func TestValidateOpenAISSECompleteness(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 完整的OpenAI流（有[DONE]和finish_reason）
	completeSSE := []byte(`data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`)

	err := validator.validateOpenAISSECompleteness(completeSSE, "https://api.openai.com")
	if err != nil {
		t.Errorf("Expected complete OpenAI SSE to pass validation, got error: %v", err)
	}

	// 测试用例2: 缺少[DONE]标记
	noDoneSSE := []byte(`data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}]}

`)

	err = validator.validateOpenAISSECompleteness(noDoneSSE, "https://api.openai.com")
	if err == nil {
		t.Error("Expected OpenAI SSE without [DONE] to fail validation")
	}
	if !contains(err.Error(), "missing [DONE]") {
		t.Errorf("Expected error message to contain 'missing [DONE]', got: %v", err)
	}

	// apis.iflow.cn 缺少[DONE]应通过
	err = validator.validateOpenAISSECompleteness(noDoneSSE, "https://apis.iflow.cn")
	if err != nil {
		t.Errorf("Expected OpenAI SSE without [DONE] from apis.iflow.cn to pass validation, got: %v", err)
	}

	// 测试用例3: 缺少finish_reason
	noFinishReasonSSE := []byte(`data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: [DONE]

`)

	err = validator.validateOpenAISSECompleteness(noFinishReasonSSE, "https://api.openai.com")
	if err == nil {
		t.Error("Expected OpenAI SSE without finish_reason to fail validation")
	}
	if !contains(err.Error(), "missing finish_reason") {
		t.Errorf("Expected error message to contain 'missing finish_reason', got: %v", err)
	}

	// 测试用例4: finish_reason为null但有其他chunk包含有效finish_reason
	mixedFinishReasonSSE := []byte(`data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":" World"},"finish_reason":"length"}]}

data: [DONE]

`)

	err = validator.validateOpenAISSECompleteness(mixedFinishReasonSSE, "https://api.openai.com")
	if err != nil {
		t.Errorf("Expected OpenAI SSE with mixed finish_reason to pass validation, got error: %v", err)
	}
}

func TestValidateResponseWithPathStreamingIntegration(t *testing.T) {
	validator := NewResponseValidator()

	// 测试用例1: 完整的Anthropic SSE流应该通过集成验证
	completeAnthropicSSE := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","usage":{"input_tokens":100,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`)

	err := validator.ValidateResponseWithPath(completeAnthropicSSE, true, "anthropic", "/v1/messages", "https://api.anthropic.com")
	if err != nil {
		t.Errorf("Expected complete Anthropic SSE to pass integrated validation, got error: %v", err)
	}

	// 测试用例2: 不完整的Anthropic SSE流应该在集成验证中失败
	incompleteAnthropicSSE := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","usage":{"input_tokens":100,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0}

`)

	err = validator.ValidateResponseWithPath(incompleteAnthropicSSE, true, "anthropic", "/v1/messages", "https://api.anthropic.com")
	if err == nil {
		t.Error("Expected incomplete Anthropic SSE to fail integrated validation")
	}
	if !contains(err.Error(), "incomplete SSE stream") {
		t.Errorf("Expected error message to contain 'incomplete SSE stream', got: %v", err)
	}

	// 测试用例3: 完整的OpenAI SSE流应该通过集成验证
	completeOpenAISSE := []byte(`data: {"id":"chatcmpl-123","model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}]}

data: [DONE]

`)

	err = validator.ValidateResponseWithPath(completeOpenAISSE, true, "openai", "/v1/chat/completions", "https://api.openai.com")
	if err != nil {
		t.Errorf("Expected complete OpenAI SSE to pass integrated validation, got error: %v", err)
	}

	// 测试用例4: 不完整的OpenAI SSE流应该在集成验证中失败
	incompleteOpenAISSE := []byte(`data: {"id":"chatcmpl-123","model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

`)

	err = validator.ValidateResponseWithPath(incompleteOpenAISSE, true, "openai", "/v1/chat/completions", "https://api.openai.com")
	if err == nil {
		t.Error("Expected incomplete OpenAI SSE to fail integrated validation")
	}
	if !contains(err.Error(), "incomplete") {
		t.Errorf("Expected error message to contain 'incomplete', got: %v", err)
	}

	// 构造缺少[DONE]但包含finish_reason的流
	noDoneButFinishedOpenAISSE := []byte(`data: {"id":"chatcmpl-123","model":"gpt-3.5-turbo","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}]}` + "\n" + "\n")

	// apis.iflow.cn 缺少[DONE]但有finish_reason集成验证应通过
	err = validator.ValidateResponseWithPath(noDoneButFinishedOpenAISSE, true, "openai", "/v1/chat/completions", "https://apis.iflow.cn")
	if err != nil {
		t.Errorf("Expected OpenAI SSE without [DONE] from apis.iflow.cn to pass integrated validation, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsAt(s, substr))))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
