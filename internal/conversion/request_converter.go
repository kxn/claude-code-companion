package conversion

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"claude-code-companion/internal/logger"
)

// RequestConverter 请求转换器 - 基于参考实现
type RequestConverter struct {
	logger *logger.Logger
}

// NewRequestConverter 创建请求转换器
func NewRequestConverter(logger *logger.Logger) *RequestConverter {
	return &RequestConverter{
		logger: logger,
	}
}

// Convert 转换 Anthropic 请求为 OpenAI 格式 - 基于参考实现
func (c *RequestConverter) Convert(anthropicReq []byte, endpointInfo *EndpointInfo) ([]byte, *ConversionContext, error) {
	// 解析 Anthropic 请求
	var anthReq AnthropicRequest
	if err := json.Unmarshal(anthropicReq, &anthReq); err != nil {
		return nil, nil, NewConversionError("parse_error", "Failed to parse Anthropic request", err)
	}

	// 创建转换上下文
	ctx := &ConversionContext{
		ToolCallIDMap:  make(map[string]string),
		IsStreaming:    anthReq.Stream != nil && *anthReq.Stream,
		RequestHeaders: make(map[string]string),
		StopSequences:  anthReq.StopSequences,
	}

	// 构建 OpenAI 请求
	out := OpenAIRequest{
		Model: anthReq.Model,
	}

	// 温控映射
	out.Temperature = anthReq.Temperature
	out.TopP = anthReq.TopP

	// 根据端点配置处理 max_tokens 字段名转换
	if endpointInfo != nil && endpointInfo.MaxTokensFieldName != "" {
		// 根据配置的字段名设置对应字段
		switch endpointInfo.MaxTokensFieldName {
		case "max_completion_tokens":
			out.MaxCompletionTokens = anthReq.MaxTokens
		case "max_output_tokens":
			out.MaxOutputTokens = anthReq.MaxTokens
		default:
			// 默认或未知值，保持原始字段名
			out.MaxTokens = anthReq.MaxTokens
		}
	} else {
		// 没有配置时使用默认行为：保持原始字段名
		out.MaxTokens = anthReq.MaxTokens
	}
	out.Stream = anthReq.Stream
	out.Stop = anthReq.StopSequences

	// 处理用户ID
	if anthReq.Metadata != nil {
		if userID, ok := anthReq.Metadata["user_id"].(string); ok && userID != "" {
			out.User = userID
		}
	}

	// 工具映射
	for _, t := range anthReq.Tools {
		out.Tools = append(out.Tools, OpenAITool{
			Type: "function",
			Function: OpenAIFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema, // JSON Schema 原样给到 parameters
			},
		})
	}

	// tool_choice 映射 - 只有在有工具时才设置
	if len(anthReq.Tools) > 0 {
		if anthReq.ToolChoice != nil {
			switch anthReq.ToolChoice.Type {
			case "auto":
				out.ToolChoice = "auto"
			case "any":
				// OpenAI 没有"any"语义；你可以：
				// 方案 A：用 "required" 强制必须走工具（更贴近"有就用"）
				// 方案 B：用 "auto"（由模型自己判断）
				// 这里选择更"强"的 A，避免模型直接文本结束：
				out.ToolChoice = "required"
			case "tool":
				out.ToolChoice = map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name": anthReq.ToolChoice.Name,
					},
				}
			default:
				out.ToolChoice = "auto"
			}
		} else {
			// 当有工具但没有指定 tool_choice 时，默认为 "auto"
			out.ToolChoice = "auto"
		}
	}
	// 如果没有工具，不设置 tool_choice

	// System 映射（可选）
	if s := c.anthropicSystemToText(anthReq.System); s != "" {
		out.Messages = append(out.Messages, OpenAIMessage{
			Role:    "system",
			Content: s,
		})
	}

	// 为了将 tool_result 正确串联到对应的 tool_call_id，
	// 需要一个（工具名 -> 最近一次生成的 call id）的映射（当上游历史中包含过 tool_use）
	latestCallIDByName := map[string]string{}

	// 遍历对话消息，逐条转换
	for _, m := range anthReq.Messages {
		switch m.Role {
		case "user":
			// 用户消息可以包含 text / image / tool_result
			// 其中 tool_result 需转成 role:"tool"
			// 其他（text/image）转为 role:"user"
			//
			// 重要：为了确保相同 ID 的 assistant 和 tool 消息紧挨着，
			// 我们需要先输出所有 tool_result，然后再输出 user 消息
			// 使用新的 GetContentBlocks 方法获取内容块
			contentBlocks := m.GetContentBlocks()
			var userBlocks []AnthropicContentBlock
			var toolResults []AnthropicContentBlock
			for _, bl := range contentBlocks {
				if bl.Type == "tool_result" {
					toolResults = append(toolResults, bl)
				} else {
					userBlocks = append(userBlocks, bl)
				}
			}

			// 先处理 tool_result -> role:"tool"
			// 这样确保 assistant 和 tool 消息紧挨着
			for _, tr := range toolResults {
				if tr.ToolUseID == "" {
					// 如果没有 tool_use_id，尝试退化策略：用工具名匹配最新 id
					// 这需要 Claude Code 侧把名字塞在 content[0].text 或外部上下文；这里尽量保守
					// 因上下文不可靠，这里严格要求 tool_use_id 存在：
					return nil, nil, errors.New("user.tool_result is missing tool_use_id")
				}

				// 提取 tool_result 的内容
				var content string
				switch v := tr.Content.(type) {
				case string:
					// content 是字符串，直接使用
					content = v
				case []AnthropicContentBlock:
					// content 是 AnthropicContentBlock 数组，提取文本
					var sb strings.Builder
					for _, c := range v {
						if c.Type == "text" {
							sb.WriteString(c.Text)
						}
					}
					content = sb.String()
				case []interface{}:
					// content 是 interface{} 数组，尝试提取文本
					var sb strings.Builder
					for _, item := range v {
						if blockMap, ok := item.(map[string]interface{}); ok {
							if typ, exists := blockMap["type"].(string); exists && typ == "text" {
								if text, exists := blockMap["text"].(string); exists {
									sb.WriteString(text)
								}
							}
						}
					}
					content = sb.String()
				default:
					content = ""
				}

				out.Messages = append(out.Messages, OpenAIMessage{
					Role:       "tool",
					ToolCallID: tr.ToolUseID,
					Content:    strings.TrimSpace(content),
				})
			}

			// 然后处理 user 内容（text/image）
			if len(userBlocks) > 0 {
				om := OpenAIMessage{Role: "user"}
				var oaParts []OpenAIMessageContent
				var sb strings.Builder // 拼接纯文本（当没有图片时可直接用字符串）
				hasImage := false
				for _, bl := range userBlocks {
					switch bl.Type {
					case "text":
						sb.WriteString(bl.Text)
					case "image":
						if bl.Source != nil && strings.EqualFold(bl.Source.Type, "base64") {
							// 有图片必须走数组 content
							hasImage = true
							oaParts = append(oaParts, OpenAIMessageContent{
								Type: "image_url",
								ImageURL: &OpenAIImageURL{
									URL: c.makeDataURL(bl.Source.MediaType, bl.Source.Data),
								},
							})
						}
					}
				}
				if hasImage {
					// 将已有文本（若有）也塞进 parts
					txt := strings.TrimSpace(sb.String())
					if txt != "" {
						oaParts = append(oaParts, OpenAIMessageContent{
							Type: "text",
							Text: txt,
						})
					}
					om.Content = oaParts
				} else {
					om.Content = strings.TrimSpace(sb.String())
				}
				if om.Content != "" {
					out.Messages = append(out.Messages, om)
				}
			}

		case "assistant":
			// assistant 可以包含：text + tool_use（一个或多个）
			// 使用新的 GetContentBlocks 方法获取内容块
			contentBlocks := m.GetContentBlocks()
			var textParts []string
			var toolUses []AnthropicContentBlock
			for _, bl := range contentBlocks {
				switch bl.Type {
				case "text":
					if bl.Text != "" {
						textParts = append(textParts, bl.Text)
					}
				case "tool_use":
					toolUses = append(toolUses, bl)
				}
			}
			om := OpenAIMessage{
				Role: "assistant",
			}
			// 文本合并
			if len(textParts) > 0 {
				om.Content = strings.Join(textParts, "\n")
			} else {
				om.Content = "" // OpenAI 允许空字符串
			}
			// tool_use -> tool_calls
			for _, tu := range toolUses {
				// OpenAI 需要 arguments 是"字符串化"的 JSON
				args := string(tu.Input)
				if !json.Valid([]byte(args)) {
					// 如果上游不是 JSON，兜个底：包一层字符串
					b, _ := json.Marshal(string(tu.Input))
					args = string(b)
				}
				call := OpenAIToolCall{
					ID:   tu.ID, // 用原始 id，便于和后续 tool_result 对齐
					Type: "function",
					Function: OpenAIToolCallDetail{
						Name:      tu.Name,
						Arguments: args,
					},
				}
				om.ToolCalls = append(om.ToolCalls, call)
				// 记录最近 callId，供可能的降级匹配（此处只做示例）
				latestCallIDByName[tu.Name] = tu.ID
			}
			out.Messages = append(out.Messages, om)

		default:
			// 其它角色（理论上 Anthropic 就 user/assistant）
			// 忽略或报错，这里选择忽略
		}
	}

	// 处理并行工具调用设置
	if anthReq.DisableParallelToolUse != nil && *anthReq.DisableParallelToolUse {
		out.ParallelToolCalls = boolPtr(false)
	}

	// 处理 thinking 模式转换为 OpenAI 推理模式
	if anthReq.Thinking != nil && anthReq.Thinking.Type == "enabled" {
		// 根据 budget_tokens 映射推理强度
		if anthReq.Thinking.BudgetTokens > 0 {
			out.MaxReasoningTokens = &anthReq.Thinking.BudgetTokens

			// 根据 budget_tokens 的大小设置推理强度
			if anthReq.Thinking.BudgetTokens <= 5000 {
				out.ReasoningEffort = stringPtr("low")
			} else if anthReq.Thinking.BudgetTokens <= 15000 {
				out.ReasoningEffort = stringPtr("medium")
			} else {
				out.ReasoningEffort = stringPtr("high")
			}
		} else {
			// 如果没有指定 budget_tokens，使用默认的 medium 强度
			out.ReasoningEffort = stringPtr("medium")
		}

		if c.logger != nil {
			c.logger.Debug("Converted thinking mode to OpenAI reasoning mode", map[string]interface{}{
				"budget_tokens":    anthReq.Thinking.BudgetTokens,
				"reasoning_effort": *out.ReasoningEffort,
			})
		}
	}

	// 记录忽略的字段
	if c.logger != nil {
		if anthReq.TopK != nil {
			c.logger.Debug("Ignoring top_k field (not supported by OpenAI)")
		}
	}

	// 如果需要重排 system 消息到最前面
	if endpointInfo != nil && endpointInfo.ReorderSystemMessagesFirst {
		out.Messages = reorderSystemMessagesFirst(out.Messages)
		if c.logger != nil {
			c.logger.Debug("Reordered system messages to the front of messages array")
		}
	}

	// 序列化结果
	result, err := json.Marshal(out)
	if err != nil {
		return nil, nil, NewConversionError("marshal_error", "Failed to marshal OpenAI request", err)
	}

	if c.logger != nil {
		c.logger.Debug("Request conversion completed")
	}

	return result, ctx, nil
}

// boolPtr 返回bool指针
func boolPtr(b bool) *bool {
	return &b
}

// stringPtr 返回string指针
func stringPtr(s string) *string {
	return &s
}

// makeDataURL 将 Anthropic Image(base64) 转成 OpenAI data URL
func (c *RequestConverter) makeDataURL(mediaType, b64 string) string {
	// 尝试粗验 b64：非严格必要
	if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
		// 如果不是纯 b64（比如已带 data: 前缀），直接原样返回
		return b64
	}
	return fmt.Sprintf("data:%s;base64,%s", mediaType, b64)
}

// anthropicSystemToText 将可能为 string 或 []AnthropicContentBlock 的 system 收敛为纯文本（保守策略）
func (c *RequestConverter) anthropicSystemToText(sys interface{}) string {
	switch v := sys.(type) {
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, it := range v {
			// 只抽取 text，忽略非 text（比如 image）
			if m, ok := it.(map[string]interface{}); ok {
				if t, _ := m["type"].(string); t == "text" {
					if tx, _ := m["text"].(string); tx != "" {
						sb.WriteString(tx)
						sb.WriteString("\n")
					}
				}
			}
		}
		return strings.TrimSpace(sb.String())
	default:
		// 尝试按 AnthropicContentBlock 反序列
		b, _ := json.Marshal(v)
		var blocks []AnthropicContentBlock
		if err := json.Unmarshal(b, &blocks); err == nil {
			var sb strings.Builder
			for _, bl := range blocks {
				if bl.Type == "text" && bl.Text != "" {
					sb.WriteString(bl.Text)
					sb.WriteString("\n")
				}
			}
			return strings.TrimSpace(sb.String())
		}
		return ""
	}
}

// reorderSystemMessagesFirst 将所有 system 消息重排到 messages 数组的最前面
// 保持 system 消息之间的原有顺序，以及非 system 消息之间的原有顺序
func reorderSystemMessagesFirst(messages []OpenAIMessage) []OpenAIMessage {
	if len(messages) == 0 {
		return messages
	}

	var systemMessages []OpenAIMessage
	var nonSystemMessages []OpenAIMessage

	// 分离 system 消息和非 system 消息
	for _, msg := range messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		} else {
			nonSystemMessages = append(nonSystemMessages, msg)
		}
	}

	// 如果没有 system 消息，直接返回原数组
	if len(systemMessages) == 0 {
		return messages
	}

	// 将 system 消息放在最前面，然后是非 system 消息
	result := make([]OpenAIMessage, 0, len(messages))
	result = append(result, systemMessages...)
	result = append(result, nonSystemMessages...)

	return result
}
