package proxy

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"claude-code-companion/internal/conversion"
	"claude-code-companion/internal/endpoint"
	"claude-code-companion/internal/tagging"
	"claude-code-companion/internal/utils"

	"github.com/gin-gonic/gin"
)

func (s *Server) proxyToEndpoint(c *gin.Context, ep *endpoint.Endpoint, path string, requestBody []byte, requestID string, startTime time.Time, taggedRequest *tagging.TaggedRequest, attemptNumber int) (bool, bool) {
	// 检查是否为 count_tokens 请求到 OpenAI 端点
	isCountTokensRequest := strings.Contains(path, "/count_tokens")
	isOpenAIEndpoint := ep.EndpointType == "openai"

	// OpenAI 端点不支持 count_tokens，立即尝试下一个端点
	if isCountTokensRequest && isOpenAIEndpoint {
		s.logger.Debug(fmt.Sprintf("Skipping count_tokens request on OpenAI endpoint %s", ep.Name))
		// 标记这次尝试为特殊情况，不记录健康统计，不记录日志（除非所有端点都因此失败）
		c.Set("skip_health_record", true)
		c.Set("skip_logging", true)
		c.Set("count_tokens_openai_skip", true)
		c.Set("last_error", fmt.Errorf("count_tokens not supported on OpenAI endpoint"))
		c.Set("last_status_code", http.StatusNotFound)
		return false, true // 立即尝试下一个端点
	}
	// 为这个端点记录独立的开始时间
	endpointStartTime := time.Now()
	targetURL := ep.GetFullURL(path)

	// Extract tags from taggedRequest
	var tags []string
	if taggedRequest != nil {
		tags = taggedRequest.Tags
	}

	// 创建HTTP请求用于模型重写处理
	tempReq, err := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		s.logger.Error("Failed to create request", err)
		// 记录创建请求失败的日志
		duration := time.Since(endpointStartTime)
		createRequestError := fmt.Sprintf("Failed to create request: %v", err)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, requestBody, c, nil, nil, nil, duration, fmt.Errorf(createRequestError), false, tags, "", "", "", attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", fmt.Errorf(createRequestError))
		c.Set("last_status_code", 0)
		return false, false
	}

	// 应用模型重写（如果配置了）
	originalModel, rewrittenModel, err := s.modelRewriter.RewriteRequestWithTags(tempReq, ep.ModelRewrite, ep.Tags)
	if err != nil {
		s.logger.Error("Model rewrite failed", err)
		// 记录模型重写失败的日志
		duration := time.Since(endpointStartTime)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, requestBody, c, nil, nil, nil, duration, err, false, tags, "", "", "", attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", err)
		c.Set("last_status_code", 0)
		return false, false
	}

	// 如果进行了模型重写，获取重写后的请求体
	var finalRequestBody []byte
	if originalModel != "" && rewrittenModel != "" {
		finalRequestBody, err = io.ReadAll(tempReq.Body)
		if err != nil {
			s.logger.Error("Failed to read rewritten request body", err)
			duration := time.Since(endpointStartTime)
			s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, nil, nil, nil, duration, err, false, tags, "", originalModel, rewrittenModel, attemptNumber)
			// 设置错误信息到context中
			c.Set("last_error", err)
			c.Set("last_status_code", 0)
			return false, false
		}
	} else {
		finalRequestBody = requestBody // 使用原始请求体
	}

	// 格式转换（在模型重写之后）
	var conversionContext *conversion.ConversionContext
	if s.converter.ShouldConvert(ep.EndpointType) {
		s.logger.Info(fmt.Sprintf("Starting request conversion for endpoint type: %s", ep.EndpointType))

		// 创建端点信息
		endpointInfo := &conversion.EndpointInfo{
			Type:               ep.EndpointType,
			MaxTokensFieldName: ep.MaxTokensFieldName,
		}

		convertedBody, ctx, err := s.converter.ConvertRequest(finalRequestBody, endpointInfo)
		if err != nil {
			s.logger.Error("Request format conversion failed", err)
			duration := time.Since(endpointStartTime)
			s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, nil, nil, nil, duration, err, false, tags, "", originalModel, rewrittenModel, attemptNumber)
			// Request转换失败是请求格式问题，不应该重试其他端点，直接返回错误
			c.JSON(http.StatusBadRequest, gin.H{"error": "Request format conversion failed", "details": err.Error()})
			// 设置错误信息到context中
			c.Set("last_error", err)
			c.Set("last_status_code", http.StatusBadRequest)
			return false, false // 不重试，直接返回
		}
		finalRequestBody = convertedBody
		conversionContext = ctx
		s.logger.Debug("Request format converted successfully", map[string]interface{}{
			"endpoint_type":  ep.EndpointType,
			"original_size":  len(requestBody),
			"converted_size": len(convertedBody),
		})
	}

	// OpenAI user 参数长度限制 hack（在格式转换之后，参数覆盖之前）
	if ep.EndpointType == "openai" {
		hackedBody, err := s.applyOpenAIUserLengthHack(finalRequestBody)
		if err != nil {
			s.logger.Debug("Failed to apply OpenAI user length hack", map[string]interface{}{
				"error": err.Error(),
			})
			// 不返回错误，继续使用原始请求体
		} else if hackedBody != nil {
			finalRequestBody = hackedBody
			s.logger.Debug("OpenAI user parameter length hack applied")
		}

		// GPT-5 模型特殊处理 hack
		gpt5HackedBody, err := s.applyGPT5ModelHack(finalRequestBody)
		if err != nil {
			s.logger.Debug("Failed to apply GPT-5 model hack", map[string]interface{}{
				"error": err.Error(),
			})
			// 不返回错误，继续使用原始请求体
		} else if gpt5HackedBody != nil {
			finalRequestBody = gpt5HackedBody
			s.logger.Debug("GPT-5 model hack applied")
		}
	}

	// 应用请求参数覆盖规则（在格式转换之后，创建HTTP请求之前）
	if parameterOverrides := ep.GetParameterOverrides(); parameterOverrides != nil && len(parameterOverrides) > 0 {
		overriddenBody, err := s.applyParameterOverrides(finalRequestBody, parameterOverrides)
		if err != nil {
			s.logger.Debug("Failed to apply parameter overrides", map[string]interface{}{
				"error": err.Error(),
			})
			// 不返回错误，继续使用原始请求体
		} else {
			finalRequestBody = overriddenBody
			s.logger.Info("Request parameter overrides applied", map[string]interface{}{
				"endpoint":        ep.Name,
				"overrides_count": len(parameterOverrides),
			})
		}
	}

	// 创建最终的HTTP请求
	req, err := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(finalRequestBody))
	if err != nil {
		s.logger.Error("Failed to create final request", err)
		// 记录创建请求失败的日志
		duration := time.Since(endpointStartTime)
		createRequestError := fmt.Sprintf("Failed to create final request: %v", err)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, nil, nil, nil, duration, fmt.Errorf(createRequestError), false, tags, "", originalModel, rewrittenModel, attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", fmt.Errorf(createRequestError))
		c.Set("last_status_code", 0)
		return false, false
	}

	for key, values := range c.Request.Header {
		if key == "Authorization" {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// 根据认证类型设置不同的认证头部
	if ep.AuthType == "api_key" {
		req.Header.Set("x-api-key", ep.AuthValue)
	} else {
		authHeader, err := ep.GetAuthHeaderWithRefreshCallback(s.config.Timeouts.ToProxyTimeoutConfig(), s.createOAuthTokenRefreshCallback())
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to get auth header: %v", err), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			// 设置错误信息到context中
			c.Set("last_error", err)
			c.Set("last_status_code", http.StatusUnauthorized)
			return false, false
		}
		req.Header.Set("Authorization", authHeader)
	}

	// Special OAuth header hack for api.anthropic.com with OAuth tokens
	if strings.Contains(ep.URL, "api.anthropic.com") && ep.AuthType == "auth_token" && strings.HasPrefix(ep.AuthValue, "sk-ant-oat01") {
		if existingBeta := req.Header.Get("Anthropic-Beta"); existingBeta != "" {
			// Prepend oauth-2025-04-20 to existing Anthropic-Beta header
			req.Header.Set("Anthropic-Beta", "oauth-2025-04-20,"+existingBeta)
		} else {
			// Set oauth-2025-04-20 as the only value if no existing header
			req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
		}
	}

	// 应用HTTP Header覆盖规则（在所有其他header处理之后）
	if headerOverrides := ep.GetHeaderOverrides(); headerOverrides != nil && len(headerOverrides) > 0 {
		for headerName, headerValue := range headerOverrides {
			if headerValue == "" {
				// 空值表示删除header
				req.Header.Del(headerName)
				s.logger.Debug(fmt.Sprintf("Header override: deleted header %s for endpoint %s", headerName, ep.Name))
			} else {
				// 非空值表示设置header
				req.Header.Set(headerName, headerValue)
				s.logger.Debug(fmt.Sprintf("Header override: set header %s = [REDACTED] for endpoint %s", headerName, ep.Name))
			}
		}
	}

	if c.Request.URL.RawQuery != "" {
		req.URL.RawQuery = c.Request.URL.RawQuery
	}

	// 为这个端点创建支持代理的HTTP客户端
	client, err := ep.CreateProxyClient(s.config.Timeouts.ToProxyTimeoutConfig())
	if err != nil {
		s.logger.Error("Failed to create proxy client for endpoint", err)
		duration := time.Since(endpointStartTime)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, nil, nil, duration, err, s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", err)
		c.Set("last_status_code", 0)
		return false, true
	}

	resp, err := client.Do(req)
	if err != nil {
		duration := time.Since(endpointStartTime)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, nil, nil, duration, err, s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
		// 设置错误信息到context中，供重试逻辑使用
		c.Set("last_error", err)
		c.Set("last_status_code", 0) // 网络错误，没有状态码
		return false, true
	}
	defer resp.Body.Close()

	// 检查认证失败情况，如果是OAuth端点且有refresh_token，先尝试刷新token
	if (resp.StatusCode == 401 || resp.StatusCode == 403) &&
		ep.AuthType == "oauth" &&
		ep.OAuthConfig != nil &&
		ep.OAuthConfig.RefreshToken != "" {

		// 检查是否已经因为这个端点的认证问题刷新过token
		refreshKey := fmt.Sprintf("oauth_refresh_attempted_%s", ep.ID)
		if _, alreadyRefreshed := c.Get(refreshKey); !alreadyRefreshed {
			s.logger.Info(fmt.Sprintf("Authentication failed (HTTP %d) for OAuth endpoint %s, attempting token refresh", resp.StatusCode, ep.Name))

			// 标记我们已经为这个端点尝试过token刷新，避免无限循环
			c.Set(refreshKey, true)

			// 尝试刷新token
			if refreshErr := ep.RefreshOAuthTokenWithCallback(s.config.Timeouts.ToProxyTimeoutConfig(), s.createOAuthTokenRefreshCallback()); refreshErr != nil {
				s.logger.Error(fmt.Sprintf("Failed to refresh OAuth token for endpoint %s: %v", ep.Name, refreshErr), refreshErr)

				// 刷新失败，读取响应体用于日志记录
				duration := time.Since(endpointStartTime)
				body, _ := io.ReadAll(resp.Body)
				contentEncoding := resp.Header.Get("Content-Encoding")
				decompressedBody, err := s.validator.GetDecompressedBody(body, contentEncoding)
				if err != nil {
					decompressedBody = body // 如果解压失败，使用原始数据
				}

				s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, decompressedBody, duration, nil, s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
				// 设置错误信息到context中
				c.Set("last_error", fmt.Errorf("OAuth token refresh failed: %v", refreshErr))
				c.Set("last_status_code", resp.StatusCode)
				return false, true
			} else {
				s.logger.Info(fmt.Sprintf("OAuth token refreshed successfully for endpoint %s, retrying request", ep.Name))

				// 关闭原始响应体
				resp.Body.Close()

				// Token刷新成功，递归重试相同的endpoint（重新走完整的请求流程）
				return s.proxyToEndpoint(c, ep, path, requestBody, requestID, startTime, taggedRequest, attemptNumber)
			}
		} else {
			s.logger.Debug(fmt.Sprintf("OAuth token refresh already attempted for endpoint %s in this request, not retrying", ep.Name))
		}
	}

	// 只有2xx状态码才认为是成功，其他所有状态码都尝试下一个端点
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		duration := time.Since(endpointStartTime)
		body, _ := io.ReadAll(resp.Body)

		// 解压响应体用于日志记录
		contentEncoding := resp.Header.Get("Content-Encoding")
		decompressedBody, err := s.validator.GetDecompressedBody(body, contentEncoding)
		if err != nil {
			decompressedBody = body // 如果解压失败，使用原始数据
		}

		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, decompressedBody, duration, nil, s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
		s.logger.Debug(fmt.Sprintf("HTTP error %d from endpoint %s, trying next endpoint", resp.StatusCode, ep.Name))
		// 设置状态码到context中，供重试逻辑使用
		c.Set("last_error", nil)
		c.Set("last_status_code", resp.StatusCode)
		return false, true
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("Failed to read response body", err)
		// 记录读取响应体失败的日志
		duration := time.Since(endpointStartTime)
		readError := fmt.Sprintf("Failed to read response body: %v", err)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, nil, duration, fmt.Errorf(readError), s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", fmt.Errorf(readError))
		c.Set("last_status_code", resp.StatusCode)
		return false, false
	}

	// 解压响应体仅用于日志记录和验证
	contentEncoding := resp.Header.Get("Content-Encoding")
	decompressedBody, err := s.validator.GetDecompressedBody(responseBody, contentEncoding)
	if err != nil {
		s.logger.Error("Failed to decompress response body", err)
		// 记录解压响应体失败的日志
		duration := time.Since(endpointStartTime)
		decompressError := fmt.Sprintf("Failed to decompress response body: %v", err)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, responseBody, duration, fmt.Errorf(decompressError), s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", fmt.Errorf(decompressError))
		c.Set("last_status_code", resp.StatusCode)
		return false, false
	}

	// 智能检测内容类型并自动覆盖
	currentContentType := resp.Header.Get("Content-Type")
	newContentType, overrideInfo := s.validator.SmartDetectContentType(decompressedBody, currentContentType, resp.StatusCode)

	// 确定最终的Content-Type和是否为流式响应
	finalContentType := currentContentType
	if newContentType != "" {
		finalContentType = newContentType
		s.logger.Info(fmt.Sprintf("Auto-detected content type mismatch for endpoint %s: %s", ep.Name, overrideInfo))
	}

	// 判断是否为流式响应（基于最终的Content-Type）
	isStreaming := strings.Contains(strings.ToLower(finalContentType), "text/event-stream")

	// 复制响应头，但跳过可能需要重新计算的头部
	for key, values := range resp.Header {
		keyLower := strings.ToLower(key)
		if keyLower == "content-type" && newContentType != "" {
			c.Header(key, finalContentType)
		} else if keyLower == "content-length" || keyLower == "content-encoding" {
			// 这些头部会在后面根据最终响应体重新设置
			continue
		} else {
			for _, value := range values {
				c.Header(key, value)
			}
		}
	}

	// 监控Anthropic rate limit headers
	if ep.ShouldMonitorRateLimit() {
		if err := s.processRateLimitHeaders(ep, resp.Header, requestID); err != nil {
			s.logger.Error("Failed to process rate limit headers", err)
		}
	}

	// 严格 Anthropic 格式验证已永久启用
	if err := s.validator.ValidateResponseWithPath(decompressedBody, isStreaming, ep.EndpointType, path, ep.URL); err != nil {
		// 如果是usage统计验证失败，尝试下一个endpoint
		if strings.Contains(err.Error(), "invalid usage stats") {
			s.logger.Info(fmt.Sprintf("Usage validation failed for endpoint %s: %v", ep.Name, err))
			duration := time.Since(endpointStartTime)
			errorLog := fmt.Sprintf("Usage validation failed: %v", err)
			s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, append(decompressedBody, []byte(errorLog)...), duration, fmt.Errorf(errorLog), s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
			// 设置错误信息到context中
			c.Set("last_error", fmt.Errorf(errorLog))
			c.Set("last_status_code", resp.StatusCode)
			return false, true // 验证失败，尝试下一个endpoint
		}

		// 如果是SSE流不完整的验证失败，尝试下一个endpoint
		if strings.Contains(err.Error(), "incomplete SSE stream") || strings.Contains(err.Error(), "missing message_stop") || strings.Contains(err.Error(), "missing [DONE]") || strings.Contains(err.Error(), "missing finish_reason") {
			s.logger.Info(fmt.Sprintf("Incomplete SSE stream detected for endpoint %s: %v", ep.Name, err))
			duration := time.Since(endpointStartTime)
			errorLog := fmt.Sprintf("Incomplete SSE stream: %v", err)
			s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, append(decompressedBody, []byte(errorLog)...), duration, fmt.Errorf(errorLog), s.isRequestExpectingStream(req), tags, "", originalModel, rewrittenModel, attemptNumber)
			// 设置错误信息到context中
			c.Set("last_error", fmt.Errorf(errorLog))
			c.Set("last_status_code", resp.StatusCode)
			return false, true // SSE流不完整，尝试下一个endpoint
		}

		// 验证失败，尝试下一个端点
		s.logger.Info(fmt.Sprintf("Response validation failed for endpoint %s, trying next endpoint: %v", ep.Name, err))
		duration := time.Since(endpointStartTime)
		validationError := fmt.Sprintf("Response validation failed: %v", err)
		s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, decompressedBody, duration, fmt.Errorf(validationError), isStreaming, tags, "", originalModel, rewrittenModel, attemptNumber)
		// 设置错误信息到context中
		c.Set("last_error", fmt.Errorf(validationError))
		c.Set("last_status_code", resp.StatusCode)
		return false, true // 验证失败，尝试下一个endpoint
	}

	c.Status(resp.StatusCode)

	// 格式转换（在模型重写之前）
	convertedResponseBody := decompressedBody
	if conversionContext != nil {
		s.logger.Info(fmt.Sprintf("Starting response conversion. Streaming: %v, OriginalSize: %d", isStreaming, len(decompressedBody)))
		convertedResp, err := s.converter.ConvertResponse(decompressedBody, conversionContext, isStreaming)
		if err != nil {
			s.logger.Error("Response format conversion failed", err)
			// Response转换失败，记录错误并尝试下一个端点
			duration := time.Since(endpointStartTime)
			conversionError := fmt.Sprintf("Response format conversion failed: %v", err)
			s.logSimpleRequest(requestID, ep.URL, c.Request.Method, path, requestBody, finalRequestBody, c, req, resp, decompressedBody, duration, fmt.Errorf(conversionError), isStreaming, tags, "", originalModel, rewrittenModel, attemptNumber)
			// 设置错误信息到context中
			c.Set("last_error", fmt.Errorf(conversionError))
			c.Set("last_status_code", resp.StatusCode)
			return false, true // Response转换失败，尝试下一个端点
		} else {
			convertedResponseBody = convertedResp
			s.logger.Info(fmt.Sprintf("Response conversion successful! Original: %d bytes -> Converted: %d bytes", len(decompressedBody), len(convertedResp)))
			s.logger.Debug("Response format converted successfully", map[string]interface{}{
				"endpoint_type":  conversionContext.EndpointType,
				"original_size":  len(decompressedBody),
				"converted_size": len(convertedResp),
			})
		}
	}

	// 应用响应模型重写（如果进行了请求模型重写）
	finalResponseBody := convertedResponseBody
	if originalModel != "" && rewrittenModel != "" {
		rewrittenResponseBody, err := s.modelRewriter.RewriteResponse(convertedResponseBody, originalModel, rewrittenModel)
		if err != nil {
			s.logger.Error("Failed to rewrite response model", err)
			// 如果响应重写失败，使用转换后的响应体，不中断请求
		} else if len(rewrittenResponseBody) > 0 && !bytes.Equal(rewrittenResponseBody, convertedResponseBody) {
			// 如果响应重写成功且内容发生了变化，发送重写后的未压缩响应
			// 并移除Content-Encoding头（因为我们发送的是未压缩数据）
			c.Header("Content-Encoding", "")
			c.Header("Content-Length", fmt.Sprintf("%d", len(rewrittenResponseBody)))
			finalResponseBody = rewrittenResponseBody
		} else {
			// 如果没有重写或重写后内容没变化，使用转换后的响应体
			finalResponseBody = convertedResponseBody
		}
	} else if conversionContext != nil {
		// 只有格式转换没有模型重写的情况
		finalResponseBody = convertedResponseBody
	}

	// 设置正确的响应头部
	if conversionContext != nil || (originalModel != "" && rewrittenModel != "") {
		// 如果进行了转换或模型重写，需要重新设置头部
		// 移除压缩编码（因为我们发送的是解压后的数据）
		c.Header("Content-Encoding", "")
		// 设置正确的内容长度
		c.Header("Content-Length", fmt.Sprintf("%d", len(finalResponseBody)))
	}

	// 如果是流式响应，确保设置正确的SSE头部
	if isStreaming {
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no") // 防止中间层缓冲
		// 移除Content-Length头部（SSE不应该设置这个）
		c.Header("Content-Length", "")
	}

	// 发送最终响应体给客户端
	c.Writer.Write(finalResponseBody)

	// 清除错误信息（成功情况）
	c.Set("last_error", nil)
	c.Set("last_status_code", resp.StatusCode)

	duration := time.Since(endpointStartTime)
	// 创建日志条目，记录修改前后的完整数据
	requestLog := s.logger.CreateRequestLog(requestID, ep.URL, c.Request.Method, path)
	requestLog.RequestBodySize = len(requestBody)
	requestLog.Tags = tags
	requestLog.ContentTypeOverride = overrideInfo
	requestLog.AttemptNumber = attemptNumber

	// 设置 thinking 信息
	if thinkingInfo, exists := c.Get("thinking_info"); exists {
		if info, ok := thinkingInfo.(*utils.ThinkingInfo); ok && info != nil {
			requestLog.ThinkingEnabled = info.Enabled
			requestLog.ThinkingBudgetTokens = info.BudgetTokens
		}
	}

	// 记录原始客户端请求数据
	requestLog.OriginalRequestURL = c.Request.URL.String()
	requestLog.OriginalRequestHeaders = utils.HeadersToMap(c.Request.Header)
	if len(requestBody) > 0 {
		if s.config.Logging.LogRequestBody != "none" {
			if s.config.Logging.LogRequestBody == "truncated" {
				requestLog.OriginalRequestBody = utils.TruncateBody(string(requestBody), 1024)
			} else {
				requestLog.OriginalRequestBody = string(requestBody)
			}
		}
	}

	// 记录最终发送给上游的请求数据
	requestLog.FinalRequestURL = req.URL.String()
	requestLog.FinalRequestHeaders = utils.HeadersToMap(req.Header)
	if len(finalRequestBody) > 0 {
		if s.config.Logging.LogRequestBody != "none" {
			if s.config.Logging.LogRequestBody == "truncated" {
				requestLog.FinalRequestBody = utils.TruncateBody(string(finalRequestBody), 1024)
			} else {
				requestLog.FinalRequestBody = string(finalRequestBody)
			}
		}
	}

	// 记录上游原始响应数据
	requestLog.OriginalResponseHeaders = utils.HeadersToMap(resp.Header)
	if len(decompressedBody) > 0 {
		if s.config.Logging.LogResponseBody != "none" {
			if s.config.Logging.LogResponseBody == "truncated" {
				requestLog.OriginalResponseBody = utils.TruncateBody(string(decompressedBody), 1024)
			} else {
				requestLog.OriginalResponseBody = string(decompressedBody)
			}
		}
	}

	// 记录最终发送给客户端的响应数据
	finalHeaders := make(map[string]string)
	for key := range resp.Header {
		values := c.Writer.Header().Values(key)
		if len(values) > 0 {
			finalHeaders[key] = values[0]
		}
	}
	requestLog.FinalResponseHeaders = finalHeaders
	if len(finalResponseBody) > 0 {
		if s.config.Logging.LogResponseBody != "none" {
			if s.config.Logging.LogResponseBody == "truncated" {
				requestLog.FinalResponseBody = utils.TruncateBody(string(finalResponseBody), 1024)
			} else {
				requestLog.FinalResponseBody = string(finalResponseBody)
			}
		}
	}

	// 设置兼容性字段
	requestLog.RequestHeaders = requestLog.FinalRequestHeaders
	requestLog.RequestBody = requestLog.OriginalRequestBody
	requestLog.ResponseHeaders = requestLog.OriginalResponseHeaders
	requestLog.ResponseBody = requestLog.OriginalResponseBody

	// 设置模型信息
	if len(requestBody) > 0 {
		extractedModel := utils.ExtractModelFromRequestBody(string(requestBody))
		if originalModel != "" {
			requestLog.Model = originalModel
			requestLog.OriginalModel = originalModel
		} else {
			requestLog.Model = extractedModel
			requestLog.OriginalModel = extractedModel
		}

		if rewrittenModel != "" {
			requestLog.RewrittenModel = rewrittenModel
			requestLog.ModelRewriteApplied = rewrittenModel != requestLog.OriginalModel
		}

		// 提取 Session ID
		requestLog.SessionID = utils.ExtractSessionIDFromRequestBody(string(requestBody))
	}

	// 更新基本字段
	s.logger.UpdateRequestLog(requestLog, req, resp, decompressedBody, duration, nil)
	requestLog.IsStreaming = isStreaming
	s.logger.LogRequest(requestLog)

	return true, false
}

// applyParameterOverrides 应用请求参数覆盖规则
func (s *Server) applyParameterOverrides(requestBody []byte, parameterOverrides map[string]string) ([]byte, error) {
	if len(parameterOverrides) == 0 {
		return requestBody, nil
	}

	// 解析JSON请求体
	var requestData map[string]interface{}
	if err := json.Unmarshal(requestBody, &requestData); err != nil {
		// 如果解析失败，记录日志但不返回错误，使用原始请求体
		s.logger.Debug("Failed to parse request body as JSON for parameter override, using original body", map[string]interface{}{
			"error": err.Error(),
		})
		return requestBody, nil
	}

	// 应用参数覆盖规则
	modified := false
	for paramName, paramValue := range parameterOverrides {
		if paramValue == "" {
			// 空值表示删除参数
			if _, exists := requestData[paramName]; exists {
				delete(requestData, paramName)
				modified = true
				s.logger.Debug(fmt.Sprintf("Parameter override: deleted parameter %s", paramName))
			}
		} else {
			// 非空值表示设置参数
			// 尝试解析参数值为适当的类型
			var parsedValue interface{}
			if err := json.Unmarshal([]byte(paramValue), &parsedValue); err != nil {
				// 如果JSON解析失败，作为字符串处理
				parsedValue = paramValue
			}
			requestData[paramName] = parsedValue
			modified = true
			s.logger.Debug(fmt.Sprintf("Parameter override: set parameter %s = %v", paramName, parsedValue))
		}
	}

	// 如果没有修改，返回原始请求体
	if !modified {
		return requestBody, nil
	}

	// 重新序列化为JSON
	modifiedBody, err := json.Marshal(requestData)
	if err != nil {
		s.logger.Error("Failed to marshal modified request body", err)
		return requestBody, nil // 返回原始请求体
	}

	return modifiedBody, nil
}

// applyOpenAIUserLengthHack 应用 OpenAI user 参数长度限制 hack
func (s *Server) applyOpenAIUserLengthHack(requestBody []byte) ([]byte, error) {
	// 解析JSON请求体
	var requestData map[string]interface{}
	if err := json.Unmarshal(requestBody, &requestData); err != nil {
		// 如果解析失败，记录日志但不返回错误，使用原始请求体
		s.logger.Debug("Failed to parse request body as JSON for OpenAI user hack, using original body", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, nil
	}

	// 检查是否存在 user 参数
	userValue, exists := requestData["user"]
	if !exists {
		return nil, nil // 没有 user 参数，无需处理
	}

	// 转换为字符串
	userStr, ok := userValue.(string)
	if !ok {
		return nil, nil // user 参数不是字符串，无需处理
	}

	// 检查长度（以字节为单位）
	if len(userStr) <= 64 {
		return nil, nil // 长度在限制内，无需处理
	}

	// 生成 hash
	hasher := md5.New()
	hasher.Write([]byte(userStr))
	hashBytes := hasher.Sum(nil)
	hashStr := hex.EncodeToString(hashBytes)

	// 添加前缀标识
	hashedUser := "hashed-" + hashStr

	// 更新请求数据
	requestData["user"] = hashedUser

	s.logger.Info("OpenAI user parameter hashed due to length limit", map[string]interface{}{
		"original_length": len(userStr),
		"hashed_length":   len(hashedUser),
		"original_user":   userStr[:min(32, len(userStr))] + "...", // 只记录前32个字符用于调试
	})

	// 重新序列化为JSON
	modifiedBody, err := json.Marshal(requestData)
	if err != nil {
		s.logger.Error("Failed to marshal request body after user hash", err)
		return nil, err
	}

	return modifiedBody, nil
}

// applyGPT5ModelHack 应用 GPT-5 模型特殊处理 hack
// 如果模型名包含 "gpt5" 且端点是 OpenAI 类型，则：
// 1. 如果 temperature 不是 1 则将其改为 1
// 2. 如果包含 max_tokens 字段，则将其改名为 max_completion_tokens
func (s *Server) applyGPT5ModelHack(requestBody []byte) ([]byte, error) {
	// 解析JSON请求体
	var requestData map[string]interface{}
	if err := json.Unmarshal(requestBody, &requestData); err != nil {
		// 如果解析失败，记录日志但不返回错误，使用原始请求体
		s.logger.Debug("Failed to parse request body as JSON for GPT-5 hack, using original body", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, nil
	}

	// 检查是否为 GPT-5 模型
	modelValue, exists := requestData["model"]
	if !exists {
		return nil, nil // 没有 model 参数，无需处理
	}

	modelStr, ok := modelValue.(string)
	if !ok {
		return nil, nil // model 参数不是字符串，无需处理
	}

	// 检查模型名是否包含 "gpt-5"（不区分大小写）
	if !strings.Contains(strings.ToLower(modelStr), "gpt-5") {
		return nil, nil // 不是 GPT-5 模型，无需处理
	}

	modified := false
	var hackDetails []string

	// 1. 检查并修改 temperature
	if tempValue, exists := requestData["temperature"]; exists {
		if temp, ok := tempValue.(float64); ok && temp != 1.0 {
			requestData["temperature"] = 1.0
			modified = true
			hackDetails = append(hackDetails, fmt.Sprintf("temperature: %.3f → 1.0", temp))
		}
	} else {
		// 如果没有 temperature，设置为 1.0
		requestData["temperature"] = 1.0
		modified = true
		hackDetails = append(hackDetails, "temperature: not set → 1.0")
	}

	// 2. 检查并重命名 max_tokens 为 max_completion_tokens
	if maxTokensValue, exists := requestData["max_tokens"]; exists {
		// 将 max_tokens 改名为 max_completion_tokens
		requestData["max_completion_tokens"] = maxTokensValue
		delete(requestData, "max_tokens")
		modified = true
		hackDetails = append(hackDetails, fmt.Sprintf("max_tokens → max_completion_tokens: %v", maxTokensValue))
	}

	// 如果没有修改，返回 nil
	if !modified {
		return nil, nil
	}

	s.logger.Info("GPT-5 model hack applied", map[string]interface{}{
		"model":   modelStr,
		"changes": hackDetails,
	})

	// 重新序列化为JSON
	modifiedBody, err := json.Marshal(requestData)
	if err != nil {
		s.logger.Error("Failed to marshal request body after GPT-5 hack", err)
		return nil, err
	}

	return modifiedBody, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// processRateLimitHeaders 处理Anthropic rate limit headers
func (s *Server) processRateLimitHeaders(ep *endpoint.Endpoint, headers http.Header, requestID string) error {
	resetHeader := headers.Get("Anthropic-Ratelimit-Unified-Reset")
	statusHeader := headers.Get("Anthropic-Ratelimit-Unified-Status")

	// 转换reset为int64
	var resetValue *int64
	if resetHeader != "" {
		if parsed, err := strconv.ParseInt(resetHeader, 10, 64); err == nil {
			resetValue = &parsed
		} else {
			s.logger.Debug("Failed to parse Anthropic-Ratelimit-Unified-Reset header", map[string]interface{}{
				"value":      resetHeader,
				"error":      err.Error(),
				"endpoint":   ep.Name,
				"request_id": requestID,
			})
		}
	}

	var statusValue *string
	if statusHeader != "" {
		statusValue = &statusHeader
	}

	// 更新endpoint状态
	changed, err := ep.UpdateRateLimitState(resetValue, statusValue)
	if err != nil {
		return err
	}

	// 如果状态发生变化，持久化到配置文件
	if changed {
		s.logger.Info("Rate limit state changed, persisting to config", map[string]interface{}{
			"endpoint":   ep.Name,
			"reset":      resetValue,
			"status":     statusValue,
			"request_id": requestID,
		})

		// 持久化到配置文件
		if err := s.persistRateLimitState(ep.ID, resetValue, statusValue); err != nil {
			s.logger.Error("Failed to persist rate limit state", err)
			return err
		}
	}

	// 检查增强保护：如果启用了增强保护且状态为allowed_warning，则禁用端点
	if ep.ShouldDisableOnAllowedWarning() && ep.IsAvailable() {
		s.logger.Info("Enhanced protection triggered: disabling endpoint due to allowed_warning status", map[string]interface{}{
			"endpoint":            ep.Name,
			"status":              statusValue,
			"enhanced_protection": true,
			"request_id":          requestID,
		})
		ep.MarkInactive()
	}

	return nil
}
