package web

import (
	"fmt"
	"net/http"
	"net/url"

	"claude-code-companion/internal/config"
	"claude-code-companion/internal/i18n"
	"claude-code-companion/internal/security"

	"github.com/gin-gonic/gin"
)

// handleGetEndpoints 获取所有端点
func (s *AdminServer) handleGetEndpoints(c *gin.Context) {
	endpoints := s.endpointManager.GetAllEndpoints()
	c.JSON(http.StatusOK, gin.H{
		"endpoints": endpoints,
	})
}

// handleUpdateEndpoints 批量更新端点
func (s *AdminServer) handleUpdateEndpoints(c *gin.Context) {
	var request struct {
		Endpoints []config.EndpointConfig `json:"endpoints"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// 创建新配置，只更新端点部分
	newConfig := *s.config
	newConfig.Endpoints = request.Endpoints

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(request.Endpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update endpoints: " + err.Error(),
		})
		return
	}

	// 如果没有热更新处理器，则使用旧方式
	if s.hotUpdateHandler == nil {
		s.endpointManager.UpdateEndpoints(request.Endpoints)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Endpoints updated successfully"})
}

// handleCreateEndpoint 创建新端点
func (s *AdminServer) handleCreateEndpoint(c *gin.Context) {
	var request struct {
		Name               string              `json:"name" binding:"required"`
		URL                string              `json:"url" binding:"required"`
		EndpointType       string              `json:"endpoint_type"` // "anthropic" | "openai"
		PathPrefix         string              `json:"path_prefix"`   // OpenAI 端点的路径前缀
		AuthType           string              `json:"auth_type" binding:"required"`
		AuthValue          string              `json:"auth_value"` // OAuth时不需要
		Enabled            bool                `json:"enabled"`
		Tags               []string            `json:"tags"`
		ModelAlias         string              `json:"model_alias"`
		Proxy              *config.ProxyConfig `json:"proxy,omitempty"`               // 新增：代理配置
		OAuthConfig        *config.OAuthConfig `json:"oauth_config,omitempty"`        // 新增：OAuth配置
		HeaderOverrides    map[string]string   `json:"header_overrides,omitempty"`    // 新增：HTTP Header覆盖配置
		ParameterOverrides map[string]string   `json:"parameter_overrides,omitempty"` // 新增：Request Parameter覆盖配置
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// 添加安全验证
	if err := security.ValidateEndpointName(request.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "endpoint_name_validation_failed", "端点名称验证失败: ") + err.Error()})
		return
	}

	if err := security.ValidateURL(request.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "url_validation_failed", "URL验证失败: ") + err.Error()})
		return
	}

	if err := security.ValidateTags(request.Tags); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "tags_validation_failed", "标签验证失败: ") + err.Error()})
		return
	}

	if request.ModelAlias != "" {
		if err := security.ValidateGenericText(request.ModelAlias, 100, i18n.TCtx(c, "model_alias", "模型简称")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	if request.AuthValue != "" {
		if err := security.ValidateAuthToken(request.AuthValue); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "auth_token_validation_failed", "认证令牌验证失败: ") + err.Error()})
			return
		}
	}

	if request.PathPrefix != "" {
		if err := security.ValidateGenericText(request.PathPrefix, 200, i18n.TCtx(c, "path_prefix", "路径前缀")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// 验证auth_type
	if request.AuthType != "api_key" && request.AuthType != "auth_token" && request.AuthType != "oauth" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_type must be 'api_key', 'auth_token', or 'oauth'"})
		return
	}

	// 验证 OAuth 或传统认证配置
	if request.AuthType == "oauth" {
		if request.OAuthConfig == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "oauth_config is required when auth_type is 'oauth'"})
			return
		}
		// 验证OAuth配置
		if err := config.ValidateOAuthConfig(request.OAuthConfig, fmt.Sprintf("endpoint '%s'", request.Name)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid oauth config: " + err.Error()})
			return
		}
	} else {
		// 非 OAuth 认证需要 auth_value
		if request.AuthValue == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_value is required for non-oauth authentication"})
			return
		}
	}

	// 验证代理配置（如果提供）
	if request.Proxy != nil {
		if err := config.ValidateProxyConfig(request.Proxy, fmt.Sprintf("endpoint '%s'", request.Name)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid proxy config: " + err.Error()})
			return
		}
	}

	// 设置默认值 - 移除timeout相关逻辑

	// 获取当前所有端点
	currentEndpoints := s.config.Endpoints

	// 新端点的优先级为当前最大优先级+1
	maxPriority := 0
	for _, ep := range currentEndpoints {
		if ep.Priority > maxPriority {
			maxPriority = ep.Priority
		}
	}

	// 创建新端点配置
	if request.ModelAlias != "" {
		// 自动添加同名标签
		exists := false
		for _, t := range request.Tags {
			if t == request.ModelAlias {
				exists = true
				break
			}
		}
		if !exists {
			request.Tags = append(request.Tags, request.ModelAlias)
		}
	}

	newEndpoint := createEndpointConfigFromRequest(
		request.Name, request.URL, request.EndpointType, request.PathPrefix,
		request.AuthType, request.AuthValue,
		request.Enabled, maxPriority+1, request.Tags, request.Proxy, request.OAuthConfig, request.HeaderOverrides, request.ParameterOverrides, request.ModelAlias)
	currentEndpoints = append(currentEndpoints, newEndpoint)

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(currentEndpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create endpoint: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Endpoint created successfully",
		"endpoint": newEndpoint,
	})
}

// handleUpdateEndpoint 更新特定端点
func (s *AdminServer) handleUpdateEndpoint(c *gin.Context) {
	encodedEndpointName := c.Param("id") // 使用名称作为ID
	endpointName, err := url.PathUnescape(encodedEndpointName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid endpoint name encoding"})
		return
	}

	var request struct {
		Name               string              `json:"name"`
		URL                string              `json:"url"`
		EndpointType       string              `json:"endpoint_type"`
		PathPrefix         string              `json:"path_prefix"` // OpenAI 端点的路径前缀
		AuthType           string              `json:"auth_type"`
		AuthValue          string              `json:"auth_value"`
		Enabled            bool                `json:"enabled"`
		Tags               []string            `json:"tags"`
		ModelAlias         string              `json:"model_alias"`
		Proxy              *config.ProxyConfig `json:"proxy,omitempty"`               // 新增：代理配置
		OAuthConfig        *config.OAuthConfig `json:"oauth_config,omitempty"`        // 新增：OAuth配置
		HeaderOverrides    map[string]string   `json:"header_overrides,omitempty"`    // 新增：HTTP Header覆盖配置
		ParameterOverrides map[string]string   `json:"parameter_overrides,omitempty"` // 新增：Request Parameter覆盖配置
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// 添加安全验证
	if request.Name != "" {
		if err := security.ValidateEndpointName(request.Name); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "endpoint_name_validation_failed", "端点名称验证失败: ") + err.Error()})
			return
		}
	}

	if request.URL != "" {
		if err := security.ValidateURL(request.URL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "url_validation_failed", "URL验证失败: ") + err.Error()})
			return
		}
	}

	if len(request.Tags) > 0 {
		if err := security.ValidateTags(request.Tags); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "tags_validation_failed", "标签验证失败: ") + err.Error()})
			return
		}
	}

	if request.ModelAlias != "" {
		if err := security.ValidateGenericText(request.ModelAlias, 100, i18n.TCtx(c, "model_alias", "模型简称")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	if request.AuthValue != "" {
		if err := security.ValidateAuthToken(request.AuthValue); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "auth_token_validation_failed", "认证令牌验证失败: ") + err.Error()})
			return
		}
	}

	if request.PathPrefix != "" {
		if err := security.ValidateGenericText(request.PathPrefix, 200, i18n.TCtx(c, "path_prefix", "路径前缀")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// 验证代理配置（如果提供）
	if request.Proxy != nil {
		if err := config.ValidateProxyConfig(request.Proxy, fmt.Sprintf("endpoint '%s'", endpointName)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid proxy config: " + err.Error()})
			return
		}
	}

	// 获取当前所有端点
	currentEndpoints := s.config.Endpoints
	found := false

	for i, ep := range currentEndpoints {
		if ep.Name == endpointName {
			// 更新端点，保持原有优先级
			if request.Name != "" {
				currentEndpoints[i].Name = request.Name
			}
			if request.URL != "" {
				currentEndpoints[i].URL = request.URL
			}
			if request.EndpointType != "" {
				currentEndpoints[i].EndpointType = request.EndpointType
			}
			// 处理 PathPrefix 字段，允许设置空值（对于 Anthropic 端点）
			currentEndpoints[i].PathPrefix = request.PathPrefix
			if request.AuthType != "" {
				if request.AuthType != "api_key" && request.AuthType != "auth_token" && request.AuthType != "oauth" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "auth_type must be 'api_key', 'auth_token', or 'oauth'"})
					return
				}

				// 验证 OAuth 或传统认证配置
				if request.AuthType == "oauth" {
					if request.OAuthConfig == nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "oauth_config is required when auth_type is 'oauth'"})
						return
					}
					// 验证OAuth配置
					if err := config.ValidateOAuthConfig(request.OAuthConfig, fmt.Sprintf("endpoint '%s'", endpointName)); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid oauth config: " + err.Error()})
						return
					}

					// 检查内存中是否已有更新的 OAuth token（防止覆盖已刷新的token）
					if currentEndpoints[i].AuthType == "oauth" && currentEndpoints[i].OAuthConfig != nil {
						currentExpiresAt := currentEndpoints[i].OAuthConfig.ExpiresAt
						requestExpiresAt := request.OAuthConfig.ExpiresAt

						// 如果内存中的过期时间比 WebUI 发送的更大，说明后台已刷新token，拒绝更新
						if currentExpiresAt > requestExpiresAt && requestExpiresAt > 0 {
							c.JSON(http.StatusConflict, gin.H{
								"error":              "Cannot update OAuth config: token has been refreshed in background. Please reload the page to get the latest configuration.",
								"current_expires_at": currentExpiresAt,
								"request_expires_at": requestExpiresAt,
							})
							return
						}
					}

					// 设置OAuth配置，清空auth_value
					currentEndpoints[i].OAuthConfig = request.OAuthConfig
					currentEndpoints[i].AuthValue = ""
				} else {
					// 非 OAuth 认证，清空OAuth配置
					currentEndpoints[i].OAuthConfig = nil
					if request.AuthValue != "" {
						currentEndpoints[i].AuthValue = request.AuthValue
					}
				}
				currentEndpoints[i].AuthType = request.AuthType
			}
			currentEndpoints[i].Enabled = request.Enabled

			// 更新tags字段
			currentEndpoints[i].Tags = request.Tags

			// 自动添加同名标签
			if request.ModelAlias != "" {
				exists := false
				for _, t := range currentEndpoints[i].Tags {
					if t == request.ModelAlias {
						exists = true
						break
					}
				}
				if !exists {
					currentEndpoints[i].Tags = append(currentEndpoints[i].Tags, request.ModelAlias)
				}
			}

			// 更新模型简称
			currentEndpoints[i].ModelAlias = request.ModelAlias

			// 更新代理配置
			currentEndpoints[i].Proxy = request.Proxy

			// 更新HTTP Header覆盖配置
			currentEndpoints[i].HeaderOverrides = request.HeaderOverrides

			// 更新Request Parameter覆盖配置
			currentEndpoints[i].ParameterOverrides = request.ParameterOverrides

			found = true
			break
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Endpoint not found"})
		return
	}

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(currentEndpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update endpoint: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Endpoint updated successfully"})
}

// handleDeleteEndpoint 删除端点
func (s *AdminServer) handleDeleteEndpoint(c *gin.Context) {
	encodedEndpointName := c.Param("id") // 使用名称作为ID
	endpointName, err := url.PathUnescape(encodedEndpointName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid endpoint name encoding"})
		return
	}

	// 获取当前所有端点
	currentEndpoints := s.config.Endpoints
	newEndpoints := make([]config.EndpointConfig, 0)
	found := false

	for _, ep := range currentEndpoints {
		if ep.Name != endpointName {
			newEndpoints = append(newEndpoints, ep)
		} else {
			found = true
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Endpoint not found"})
		return
	}

	// 重新计算优先级（按数组顺序）
	for i := range newEndpoints {
		newEndpoints[i].Priority = i + 1
	}

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(newEndpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete endpoint: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Endpoint deleted successfully"})
}
