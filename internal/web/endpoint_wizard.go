package web

import (
	"net/http"

	"claude-code-companion/internal/config"
	"claude-code-companion/internal/i18n"
	"claude-code-companion/internal/security"

	"github.com/gin-gonic/gin"
)

// handleGetEndpointProfiles 获取端点预设配置列表
func (s *AdminServer) handleGetEndpointProfiles(c *gin.Context) {
	// 加载嵌入的端点预设配置
	profiles, err := config.LoadEmbeddedEndpointProfiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to load endpoint profiles: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"profiles": profiles.Profiles,
	})
}

// CreateFromWizardRequest 从向导创建端点的请求结构
type CreateFromWizardRequest struct {
	ProfileID    string `json:"profile_id" binding:"required"`
	Name         string `json:"name" binding:"required"`
	AuthValue    string `json:"auth_value" binding:"required"`
	URL          string `json:"url" binding:"required"`
	DefaultModel string `json:"default_model,omitempty"`
	ModelAlias   string `json:"model_alias,omitempty"`
}

// handleCreateEndpointFromWizard 从向导创建端点
func (s *AdminServer) handleCreateEndpointFromWizard(c *gin.Context) {
	var request CreateFromWizardRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// 加载端点预设配置
	profiles, err := config.LoadEmbeddedEndpointProfiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to load endpoint profiles: " + err.Error(),
		})
		return
	}

	// 查找指定的预设配置
	profile := profiles.GetProfileByID(request.ProfileID)
	if profile == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Profile not found: " + request.ProfileID,
		})
		return
	}

	// 安全验证
	if err := security.ValidateEndpointName(request.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "endpoint_name_validation_failed", "端点名称验证失败: ") + err.Error()})
		return
	}

	if err := security.ValidateURL(request.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "url_validation_failed", "URL验证失败: ") + err.Error()})
		return
	}

	if request.AuthValue != "" {
		if err := security.ValidateAuthToken(request.AuthValue); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TCtx(c, "auth_token_validation_failed", "认证令牌验证失败: ") + err.Error()})
			return
		}
	}

	if request.DefaultModel != "" {
		if err := security.ValidateGenericText(request.DefaultModel, 100, i18n.TCtx(c, "default_model", "默认模型")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	if request.ModelAlias != "" {
		if err := security.ValidateGenericText(request.ModelAlias, 100, i18n.TCtx(c, "model_alias", "模型简称")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// 检查默认模型要求
	if profile.RequireDefaultModel && request.DefaultModel == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Default model is required for this endpoint type",
		})
		return
	}

	// 获取现有端点列表以生成唯一名称和计算优先级
	currentEndpoints := s.config.Endpoints
	existingNames := make([]string, len(currentEndpoints))
	maxPriority := 0

	for i, ep := range currentEndpoints {
		existingNames[i] = ep.Name
		if ep.Priority > maxPriority {
			maxPriority = ep.Priority
		}
	}

	// 验证端点名称唯一性
	if s.endpointNameExists(request.Name, currentEndpoints) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Endpoint name already exists: " + request.Name,
		})
		return
	}

	// 使用预设配置创建端点配置
	newEndpoint := profile.ToEndpointConfig(request.Name, request.AuthValue, request.DefaultModel, request.URL)
	newEndpoint.Priority = maxPriority + 1
	newEndpoint.ModelAlias = request.ModelAlias
	if request.ModelAlias != "" {
		newEndpoint.Tags = append(newEndpoint.Tags, request.ModelAlias)
	}

	// 添加到端点列表
	updatedEndpoints := append(currentEndpoints, newEndpoint)

	// 使用热更新机制
	if err := s.hotUpdateEndpoints(updatedEndpoints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create endpoint: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Endpoint created successfully from wizard",
		"endpoint": newEndpoint,
	})
}

// handleGenerateEndpointName 生成唯一的端点名称
func (s *AdminServer) handleGenerateEndpointName(c *gin.Context) {
	var request struct {
		ProfileID string `json:"profile_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// 安全验证
	if err := security.ValidateGenericText(request.ProfileID, 100, i18n.TCtx(c, "profile_id", "配置ID")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取现有端点名称
	currentEndpoints := s.config.Endpoints
	existingNames := make([]string, len(currentEndpoints))
	for i, ep := range currentEndpoints {
		existingNames[i] = ep.Name
	}

	// 生成唯一名称（使用profile_id作为基础名称）
	uniqueName := config.GenerateUniqueEndpointName(request.ProfileID, existingNames)

	c.JSON(http.StatusOK, gin.H{
		"suggested_name": uniqueName,
	})
}

// endpointNameExists 检查端点名称是否已存在
func (s *AdminServer) endpointNameExists(name string, endpoints []config.EndpointConfig) bool {
	for _, ep := range endpoints {
		if ep.Name == name {
			return true
		}
	}
	return false
}

// registerEndpointWizardRoutes 注册端点向导的API路由
func (s *AdminServer) registerEndpointWizardRoutes(api *gin.RouterGroup) {
	// 获取端点预设配置列表
	api.GET("/endpoint-profiles", s.handleGetEndpointProfiles)

	// 从向导创建端点
	api.POST("/endpoints/from-wizard", s.handleCreateEndpointFromWizard)

	// 生成唯一端点名称
	api.POST("/endpoints/generate-name", s.handleGenerateEndpointName)
}
