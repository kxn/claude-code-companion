package web

import (
	"fmt"
	"net/http"
	"net/url"

	"claude-code-companion/internal/config"

	"github.com/gin-gonic/gin"
)

// handleToggleEndpoint 切换端点启用/禁用状态
func (s *AdminServer) handleToggleEndpoint(c *gin.Context) {
	encodedEndpointName := c.Param("id") // 端点名称
	endpointName, err := url.PathUnescape(encodedEndpointName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid endpoint name encoding"})
		return
	}

	var request struct {
		Enabled bool `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// 获取当前所有端点
	currentEndpoints := s.config.Endpoints
	found := false

	for i, ep := range currentEndpoints {
		if ep.Name == endpointName {
			// 更新enabled状态
			currentEndpoints[i].Enabled = request.Enabled
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
			"error": "Failed to toggle endpoint: " + err.Error(),
		})
		return
	}

	actionText := "enabled"
	if !request.Enabled {
		actionText = "disabled"
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Endpoint '%s' has been %s successfully", endpointName, actionText),
	})
}

// handleCopyEndpoint 复制端点
func (s *AdminServer) handleCopyEndpoint(c *gin.Context) {
	encodedEndpointName := c.Param("id") // 要复制的端点名称
	endpointName, err := url.PathUnescape(encodedEndpointName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid endpoint name encoding"})
		return
	}

	// 查找源端点
	var sourceEndpoint *config.EndpointConfig
	for _, ep := range s.config.Endpoints {
		if ep.Name == endpointName {
			sourceEndpoint = &ep
			break
		}
	}

	if sourceEndpoint == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Endpoint not found"})
		return
	}

	// 生成唯一的新名称
	newName := s.generateUniqueEndpointName(sourceEndpoint.Name)

	// 获取当前所有端点
	currentEndpoints := s.config.Endpoints

	// 计算新端点的优先级
	maxPriority := 0
	for _, ep := range currentEndpoints {
		if ep.Priority > maxPriority {
			maxPriority = ep.Priority
		}
	}

	// 创建新端点（复制所有属性，除了名称和优先级）
	newEndpoint := config.EndpointConfig{
		Name:         newName,
		URL:          sourceEndpoint.URL,
		EndpointType: sourceEndpoint.EndpointType,
		PathPrefix:   sourceEndpoint.PathPrefix,
		AuthType:     sourceEndpoint.AuthType,
		AuthValue:    sourceEndpoint.AuthValue,
		Enabled:      sourceEndpoint.Enabled,
		Priority:     maxPriority + 1,
		Tags:         make([]string, len(sourceEndpoint.Tags)), // 复制tags
		ModelAlias:   sourceEndpoint.ModelAlias,
	}

	// 深度复制Tags切片
	copy(newEndpoint.Tags, sourceEndpoint.Tags)

	// 深度复制ModelRewrite配置
	if sourceEndpoint.ModelRewrite != nil {
		newEndpoint.ModelRewrite = &config.ModelRewriteConfig{
			Enabled: sourceEndpoint.ModelRewrite.Enabled,
			Rules:   make([]config.ModelRewriteRule, len(sourceEndpoint.ModelRewrite.Rules)),
		}
		copy(newEndpoint.ModelRewrite.Rules, sourceEndpoint.ModelRewrite.Rules)
	}

	// 深度复制Proxy配置
	if sourceEndpoint.Proxy != nil {
		newEndpoint.Proxy = &config.ProxyConfig{
			Type:     sourceEndpoint.Proxy.Type,
			Address:  sourceEndpoint.Proxy.Address,
			Username: sourceEndpoint.Proxy.Username,
			Password: sourceEndpoint.Proxy.Password,
		}
	}

	// 添加到端点列表
	currentEndpoints = append(currentEndpoints, newEndpoint)

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(currentEndpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to copy endpoint: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Endpoint copied successfully",
		"endpoint": newEndpoint,
	})
}

// handleResetEndpointStatus 重置端点状态为正常
func (s *AdminServer) handleResetEndpointStatus(c *gin.Context) {
	encodedEndpointName := c.Param("id") // 端点名称
	endpointName, err := url.PathUnescape(encodedEndpointName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid endpoint name encoding"})
		return
	}

	// 查找端点
	var endpoint *config.EndpointConfig
	for _, ep := range s.config.Endpoints {
		if ep.Name == endpointName {
			endpoint = &ep
			break
		}
	}

	if endpoint == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Endpoint not found"})
		return
	}

	// 重置端点状态
	if err := s.endpointManager.ResetEndpointStatus(endpointName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to reset endpoint status: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Endpoint '%s' status has been reset to normal", endpointName),
	})
}

// handleReorderEndpoints 重新排序端点
func (s *AdminServer) handleReorderEndpoints(c *gin.Context) {
	var request struct {
		OrderedNames []string `json:"ordered_names" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// 获取当前所有端点
	currentEndpoints := s.config.Endpoints

	// 创建按名称索引的map
	endpointMap := make(map[string]config.EndpointConfig)
	for _, ep := range currentEndpoints {
		endpointMap[ep.Name] = ep
	}

	// 按新顺序重新排列
	newEndpoints := make([]config.EndpointConfig, 0, len(request.OrderedNames))
	for i, name := range request.OrderedNames {
		if ep, exists := endpointMap[name]; exists {
			ep.Priority = i + 1 // 优先级从1开始
			newEndpoints = append(newEndpoints, ep)
		}
	}

	// 检查是否所有端点都被包含
	if len(newEndpoints) != len(currentEndpoints) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ordered names must include all existing endpoints"})
		return
	}

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(newEndpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to reorder endpoints: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Endpoints reordered successfully"})
}
