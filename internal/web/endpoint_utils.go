package web

import (
	"fmt"

	"claude-code-companion/internal/config"
)

// saveEndpointsToConfig 将端点配置保存到配置文件
func (s *AdminServer) saveEndpointsToConfig(endpointConfigs []config.EndpointConfig) error {
	// 更新配置
	s.config.Endpoints = endpointConfigs

	// 保存到文件
	return config.SaveConfig(s.config, s.configFilePath)
}

// createEndpointConfigFromRequest 从请求创建端点配置，自动设置优先级
func createEndpointConfigFromRequest(name, url, endpointType, pathPrefix, authType, authValue string, enabled bool, priority int, tags []string, proxy *config.ProxyConfig, oauthConfig *config.OAuthConfig, headerOverrides map[string]string, parameterOverrides map[string]string, modelAlias string) config.EndpointConfig {
	// 如果没有指定endpoint_type，默认为anthropic（向后兼容）
	if endpointType == "" {
		endpointType = "anthropic"
	}

	return config.EndpointConfig{
		Name:               name,
		URL:                url,
		EndpointType:       endpointType,
		PathPrefix:         pathPrefix, // 新增：支持路径前缀
		AuthType:           authType,
		AuthValue:          authValue,
		Enabled:            enabled,
		Priority:           priority,
		Tags:               tags,
		ModelAlias:         modelAlias,
		Proxy:              proxy,              // 新增：支持代理配置
		OAuthConfig:        oauthConfig,        // 新增：支持OAuth配置
		HeaderOverrides:    headerOverrides,    // 新增：支持HTTP Header覆盖配置
		ParameterOverrides: parameterOverrides, // 新增：支持Request Parameter覆盖配置
	}
}

// generateUniqueEndpointName 生成唯一的端点名称，如果存在重名则添加数字后缀
func (s *AdminServer) generateUniqueEndpointName(baseName string) string {
	currentEndpoints := s.config.Endpoints

	// 检查基础名称是否已存在
	nameExists := func(name string) bool {
		for _, ep := range currentEndpoints {
			if ep.Name == name {
				return true
			}
		}
		return false
	}

	// 如果基础名称不存在，直接返回
	if !nameExists(baseName) {
		return baseName
	}

	// 如果存在，添加数字后缀
	counter := 1
	for {
		newName := generateEndpointNameWithSuffix(baseName, counter)
		if !nameExists(newName) {
			return newName
		}
		counter++
	}
}

// generateEndpointNameWithSuffix 生成带数字后缀的端点名称
func generateEndpointNameWithSuffix(baseName string, counter int) string {
	return generateEndpointNameFormat(baseName, counter)
}

// generateEndpointNameFormat 格式化端点名称
func generateEndpointNameFormat(baseName string, counter int) string {
	return fmt.Sprintf("%s (%d)", baseName, counter)
}
