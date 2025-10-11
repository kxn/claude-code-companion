package modelrewrite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"claude-code-companion/internal/config"
	"claude-code-companion/internal/logger"
)

// Rewriter 模型重写器
type Rewriter struct {
	logger logger.Logger
}

// NewRewriter 创建新的模型重写器
func NewRewriter(logger logger.Logger) *Rewriter {
	return &Rewriter{
		logger: logger,
	}
}

// RewriteRequest 重写请求中的模型名称
func (r *Rewriter) RewriteRequest(req *http.Request, modelRewriteConfig *config.ModelRewriteConfig) (string, string, error) {
	return r.RewriteRequestWithTags(req, modelRewriteConfig, nil)
}

// RewriteRequestWithTags 重写请求中的模型名称，支持通用端点的隐式重写规则
func (r *Rewriter) RewriteRequestWithTags(req *http.Request, modelRewriteConfig *config.ModelRewriteConfig, endpointTags []string) (string, string, error) {
	// 读取请求体
	body, err := io.ReadAll(req.Body)
	if err != nil {
		r.logger.Error("Failed to read request body for model rewrite", err)
		return "", "", nil // 跳过重写，使用原始请求
	}

	// 恢复请求体，以便后续处理
	req.Body = io.NopCloser(bytes.NewReader(body))

	// 尝试解析JSON
	var requestData map[string]interface{}
	if err := json.Unmarshal(body, &requestData); err != nil {
		r.logger.Debug("Request body is not valid JSON, skipping model rewrite")
		return "", "", nil // 非JSON请求，跳过重写
	}

	// 获取model字段
	modelField, exists := requestData["model"]
	if !exists {
		r.logger.Debug("No model field found in request, skipping model rewrite")
		return "", "", nil // 没有model字段，跳过重写
	}

	originalModel, ok := modelField.(string)
	if !ok {
		r.logger.Debug("Model field is not a string, skipping model rewrite")
		return "", "", nil // model字段不是字符串，跳过重写
	}

	// 确定重写规则
	var rules []config.ModelRewriteRule
	isGenericEndpoint := len(endpointTags) == 0
	hasExplicitRules := modelRewriteConfig != nil && modelRewriteConfig.Enabled && len(modelRewriteConfig.Rules) > 0

	if hasExplicitRules {
		// 使用显式配置的规则
		rules = modelRewriteConfig.Rules
	} else if isGenericEndpoint && !strings.HasPrefix(originalModel, "claude") {
		// 通用端点的隐式规则：非claude模型重写为claude-sonnet-4-20250514
		rules = []config.ModelRewriteRule{
			{
				SourcePattern: "*",
				TargetModel:   "claude-sonnet-4-20250929",
			},
		}
		r.logger.Debug("Applying implicit model rewrite rule for generic endpoint", map[string]interface{}{
			"original_model": originalModel,
			"target_model":   "claude-sonnet-4-20250929",
		})
	} else {
		// 没有规则应用
		return "", "", nil
	}

	// 应用重写规则
	newModel := r.applyRewriteRules(originalModel, rules)
	if newModel == originalModel {
		return "", "", nil // 没有重写，返回空字符串
	}

	// 重写model字段
	requestData["model"] = newModel
	newBody, err := json.Marshal(requestData)
	if err != nil {
		r.logger.Error("Failed to marshal request body after model rewrite", err)
		return "", "", fmt.Errorf("failed to rewrite model in request: %v", err)
	}

	// 更新请求体
	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))

	r.logger.Info("Model rewritten in request", map[string]interface{}{
		"original": originalModel,
		"new":      newModel,
	})
	return originalModel, newModel, nil
}

// RewriteResponse 重写响应中的模型名称（将重写后的模型名改回原始模型名）
func (r *Rewriter) RewriteResponse(responseBody []byte, originalModel, rewrittenModel string) ([]byte, error) {
	if originalModel == "" || rewrittenModel == "" {
		return responseBody, nil // 没有进行过重写，直接返回
	}

	// 首先尝试作为JSON处理
	var responseData map[string]interface{}
	if err := json.Unmarshal(responseBody, &responseData); err == nil {
		return r.rewriteJSONResponse(responseBody, responseData, originalModel, rewrittenModel)
	}

	// 如果不是JSON，尝试作为SSE流处理
	if r.isSSEResponse(responseBody) {
		return r.rewriteSSEResponse(responseBody, originalModel, rewrittenModel)
	}

	// 既不是JSON也不是SSE，可能是其他格式（如纯文本），尝试简单字符串替换
	return r.rewriteTextResponse(responseBody, originalModel, rewrittenModel)
}

// rewriteJSONResponse 处理JSON格式的响应
func (r *Rewriter) rewriteJSONResponse(responseBody []byte, responseData map[string]interface{}, originalModel, rewrittenModel string) ([]byte, error) {
	// 检查是否有model字段且等于重写后的模型名
	if modelField, exists := responseData["model"]; exists {
		if modelStr, ok := modelField.(string); ok && modelStr == rewrittenModel {
			responseData["model"] = originalModel
			newBody, err := json.Marshal(responseData)
			if err != nil {
				r.logger.Error("Failed to marshal response body after model rewrite", err)
				return responseBody, nil // 重写失败，返回原始内容
			}
			r.logger.Info("Model rewritten in JSON response", map[string]interface{}{
				"rewritten": rewrittenModel,
				"restored":  originalModel,
			})
			return newBody, nil
		}
	}
	return responseBody, nil
}

// isSSEResponse 检查是否为SSE格式的响应
func (r *Rewriter) isSSEResponse(responseBody []byte) bool {
	bodyStr := string(responseBody)
	// SSE响应通常以 "data: " 开头
	return strings.HasPrefix(bodyStr, "data: ") || strings.Contains(bodyStr, "\ndata: ")
}

// rewriteSSEResponse 处理SSE格式的响应
func (r *Rewriter) rewriteSSEResponse(responseBody []byte, originalModel, rewrittenModel string) ([]byte, error) {
	bodyStr := string(responseBody)
	lines := strings.Split(bodyStr, "\n")
	var modifiedLines []string
	rewriteCount := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			// 提取data后面的JSON部分
			jsonStr := strings.TrimPrefix(line, "data: ")

			// 尝试解析JSON
			var eventData map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &eventData); err == nil {
				// 递归查找并替换所有包含model字段的对象
				if r.replaceModelInObject(eventData, rewrittenModel, originalModel) {
					rewriteCount++
				}

				// 重新序列化JSON
				if newJsonBytes, err := json.Marshal(eventData); err == nil {
					modifiedLines = append(modifiedLines, "data: "+string(newJsonBytes))
				} else {
					modifiedLines = append(modifiedLines, line) // 序列化失败，保持原样
				}
			} else {
				modifiedLines = append(modifiedLines, line) // JSON解析失败，保持原样
			}
		} else {
			modifiedLines = append(modifiedLines, line) // 非data行，保持原样
		}
	}

	if rewriteCount > 0 {
		r.logger.Info("Model rewritten in SSE response", map[string]interface{}{
			"rewritten":     rewrittenModel,
			"restored":      originalModel,
			"rewrite_count": rewriteCount,
		})
	}

	return []byte(strings.Join(modifiedLines, "\n")), nil
}

// replaceModelInObject 递归查找并替换对象中的model字段
func (r *Rewriter) replaceModelInObject(obj interface{}, rewrittenModel, originalModel string) bool {
	replaced := false

	switch v := obj.(type) {
	case map[string]interface{}:
		// 检查当前层级是否有model字段
		if modelField, exists := v["model"]; exists {
			if modelStr, ok := modelField.(string); ok && modelStr == rewrittenModel {
				v["model"] = originalModel
				replaced = true
			}
		}

		// 递归检查所有嵌套对象
		for _, value := range v {
			if r.replaceModelInObject(value, rewrittenModel, originalModel) {
				replaced = true
			}
		}
	case []interface{}:
		// 递归检查数组中的所有元素
		for _, item := range v {
			if r.replaceModelInObject(item, rewrittenModel, originalModel) {
				replaced = true
			}
		}
	}

	return replaced
}

// rewriteTextResponse 处理纯文本响应（简单字符串替换）
func (r *Rewriter) rewriteTextResponse(responseBody []byte, originalModel, rewrittenModel string) ([]byte, error) {
	bodyStr := string(responseBody)

	// 只有当响应中包含重写后的模型名时才进行替换
	if strings.Contains(bodyStr, rewrittenModel) {
		newBodyStr := strings.ReplaceAll(bodyStr, rewrittenModel, originalModel)
		r.logger.Info("Model rewritten in text response", map[string]interface{}{
			"rewritten": rewrittenModel,
			"restored":  originalModel,
		})
		return []byte(newBodyStr), nil
	}

	return responseBody, nil
}

// applyRewriteRules 应用重写规则
func (r *Rewriter) applyRewriteRules(originalModel string, rules []config.ModelRewriteRule) string {
	for _, rule := range rules {
		if matched, err := filepath.Match(rule.SourcePattern, originalModel); err == nil && matched {
			r.logger.Debug("Model rewrite rule matched", map[string]interface{}{
				"original": originalModel,
				"pattern":  rule.SourcePattern,
				"target":   rule.TargetModel,
			})
			return rule.TargetModel
		}
	}
	return originalModel // 没有匹配的规则，返回原模型名
}

// TestRewriteRule 测试重写规则（用于WebUI测试功能）
func (r *Rewriter) TestRewriteRule(testModel string, rules []config.ModelRewriteRule) (string, string, bool) {
	for _, rule := range rules {
		if matched, err := filepath.Match(rule.SourcePattern, testModel); err == nil && matched {
			return rule.TargetModel, rule.SourcePattern, true
		}
	}
	return testModel, "", false
}
