package endpoint

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"claude-code-companion/internal/common/httpclient"
	"claude-code-companion/internal/config"
	"claude-code-companion/internal/interfaces"
	"claude-code-companion/internal/oauth"
	"claude-code-companion/internal/statistics"
	"claude-code-companion/internal/utils"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
	StatusChecking Status = "checking"
)

// BlacklistReason 记录端点被拉黑的原因
type BlacklistReason struct {
	// 导致失效的请求ID列表
	CausingRequestIDs []string `json:"causing_request_ids"`

	// 失效时间
	BlacklistedAt time.Time `json:"blacklisted_at"`

	// 失效时的错误信息摘要
	ErrorSummary string `json:"error_summary"`
}

// 删除不再需要的 RequestRecord 定义，因为已经移到 utils 包

type Endpoint struct {
	ID                         string                     `json:"id"`
	Name                       string                     `json:"name"`
	URL                        string                     `json:"url"`
	EndpointType               string                     `json:"endpoint_type"`         // "anthropic" | "openai" 等
	PathPrefix                 string                     `json:"path_prefix,omitempty"` // OpenAI端点的路径前缀
	AuthType                   string                     `json:"auth_type"`
	AuthValue                  string                     `json:"auth_value"`
	Enabled                    bool                       `json:"enabled"`
	Priority                   int                        `json:"priority"`
	Tags                       []string                   `json:"tags"`                                    // 新增：支持的tag列表
	ModelRewrite               *config.ModelRewriteConfig `json:"model_rewrite,omitempty"`                 // 新增：模型重写配置
	Proxy                      *config.ProxyConfig        `json:"proxy,omitempty"`                         // 新增：代理配置
	OAuthConfig                *config.OAuthConfig        `json:"oauth_config,omitempty"`                  // 新增：OAuth配置
	HeaderOverrides            map[string]string          `json:"header_overrides,omitempty"`              // 新增：HTTP Header覆盖配置
	ParameterOverrides         map[string]string          `json:"parameter_overrides,omitempty"`           // 新增：Request Parameters覆盖配置
	MaxTokensFieldName         string                     `json:"max_tokens_field_name,omitempty"`         // max_tokens 参数名转换选项
	RateLimitReset             *int64                     `json:"rate_limit_reset,omitempty"`              // Anthropic-Ratelimit-Unified-Reset
	RateLimitStatus            *string                    `json:"rate_limit_status,omitempty"`             // Anthropic-Ratelimit-Unified-Status
	EnhancedProtection         bool                       `json:"enhanced_protection,omitempty"`           // 官方帐号增强保护：allowed_warning时即禁用端点
	ReorderSystemMessagesFirst *bool                      `json:"reorder_system_messages_first,omitempty"` // 仅对OpenAI端点生效：将system消息重排到messages最前面
	Status                     Status                     `json:"status"`
	LastCheck                  time.Time                  `json:"last_check"`
	FailureCount               int                        `json:"failure_count"`
	TotalRequests              int                        `json:"total_requests"`
	SuccessRequests            int                        `json:"success_requests"`
	LastFailure                time.Time                  `json:"last_failure"`
	SuccessiveSuccesses        int                        `json:"successive_successes"` // 连续成功次数
	RequestHistory             *utils.CircularBuffer      `json:"-"`                    // 使用环形缓冲区，不导出到JSON

	// 新增：被拉黑的原因（内存中，不持久化）
	BlacklistReason *BlacklistReason `json:"-"`

	// 新增：保护 BlacklistReason 的互斥锁
	blacklistMutex sync.RWMutex

	// 新增：上次记录跳过健康检查日志的时间（用于减少日志频率）
	lastSkipLogTime time.Time `json:"-"`

	mutex sync.RWMutex
}

func NewEndpoint(cfg config.EndpointConfig) *Endpoint {
	// 如果没有指定 endpoint_type，使用统一默认值
	endpointType := config.GetStringWithDefault(cfg.EndpointType, config.Default.Endpoint.Type)

	return &Endpoint{
		ID:                         generateID(cfg.Name),
		Name:                       cfg.Name,
		URL:                        cfg.URL,
		EndpointType:               endpointType,
		PathPrefix:                 cfg.PathPrefix, // 新增：复制PathPrefix
		AuthType:                   cfg.AuthType,
		AuthValue:                  cfg.AuthValue,
		Enabled:                    config.GetBoolWithDefault(cfg.Enabled, true, config.Default.Endpoint.Enabled),
		Priority:                   config.GetIntWithDefault(cfg.Priority, config.Default.Endpoint.Priority),
		Tags:                       cfg.Tags,                       // 新增：从配置中复制tags
		ModelRewrite:               cfg.ModelRewrite,               // 新增：从配置中复制模型重写配置
		Proxy:                      cfg.Proxy,                      // 新增：从配置中复制代理配置
		OAuthConfig:                cfg.OAuthConfig,                // 新增：从配置中复制OAuth配置
		HeaderOverrides:            cfg.HeaderOverrides,            // 新增：从配置中复制HTTP Header覆盖配置
		ParameterOverrides:         cfg.ParameterOverrides,         // 新增：从配置中复制Request Parameters覆盖配置
		MaxTokensFieldName:         cfg.MaxTokensFieldName,         // 新增：从配置中复制max_tokens参数名转换选项
		RateLimitReset:             cfg.RateLimitReset,             // 新增：从配置加载rate limit reset状态
		RateLimitStatus:            cfg.RateLimitStatus,            // 新增：从配置加载rate limit status状态
		EnhancedProtection:         cfg.EnhancedProtection,         // 新增：从配置加载官方帐号增强保护设置
		ReorderSystemMessagesFirst: cfg.ReorderSystemMessagesFirst, // 新增：从配置加载system消息重排设置
		Status:                     StatusActive,
		LastCheck:                  time.Now(),
		RequestHistory:             utils.NewCircularBuffer(100, 140*time.Second), // 100个记录，140秒窗口
	}
}

// 实现 EndpointSorter 接口
func (e *Endpoint) GetPriority() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.Priority
}

func (e *Endpoint) IsEnabled() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.Enabled
}

func (e *Endpoint) GetAuthHeader() (string, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	switch e.AuthType {
	case "api_key":
		return e.AuthValue, nil // api_key 直接返回值，会用 x-api-key 头部
	case "auth_token":
		return "Bearer " + e.AuthValue, nil // auth_token 使用 Bearer 前缀
	case "oauth":
		if e.OAuthConfig == nil {
			return "", fmt.Errorf("oauth config is required for oauth auth_type")
		}

		// 检查 token 是否需要刷新
		if oauth.IsTokenExpired(e.OAuthConfig) {
			return "", fmt.Errorf("oauth token expired, refresh required")
		}

		return oauth.GetAuthorizationHeader(e.OAuthConfig), nil
	default:
		return e.AuthValue, nil
	}
}

func (e *Endpoint) GetTags() []string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	// 返回tags的副本以避免并发修改
	tags := make([]string, len(e.Tags))
	copy(tags, e.Tags)
	return tags
}

// GetHeaderOverrides 安全地获取Header覆盖配置的副本
func (e *Endpoint) GetHeaderOverrides() map[string]string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.HeaderOverrides == nil {
		return nil
	}

	// 返回HeaderOverrides的副本以避免并发修改
	overrides := make(map[string]string, len(e.HeaderOverrides))
	for k, v := range e.HeaderOverrides {
		overrides[k] = v
	}
	return overrides
}

// GetParameterOverrides 安全地获取Parameter覆盖配置的副本
func (e *Endpoint) GetParameterOverrides() map[string]string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.ParameterOverrides == nil {
		return nil
	}

	// 返回ParameterOverrides的副本以避免并发修改
	overrides := make(map[string]string, len(e.ParameterOverrides))
	for k, v := range e.ParameterOverrides {
		overrides[k] = v
	}
	return overrides
}

// ToTaggedEndpoint 将Endpoint转换为TaggedEndpoint
func (e *Endpoint) ToTaggedEndpoint() interfaces.TaggedEndpoint {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	tags := make([]string, len(e.Tags))
	copy(tags, e.Tags)

	return interfaces.TaggedEndpoint{
		Name:     e.Name,
		URL:      e.URL,
		Tags:     tags,
		Priority: e.Priority,
		Enabled:  e.Enabled,
	}
}

func (e *Endpoint) GetFullURL(path string) string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	// 直接使用端点的URL作为基础URL
	baseURL := e.URL

	// 根据端点类型自动添加正确的路径前缀
	switch e.EndpointType {
	case "anthropic":
		// Anthropic 端点需要添加 /v1 前缀，因为路由组已经消费了 /v1
		return baseURL + "/v1" + path
	case "openai":
		// OpenAI 端点使用配置的路径前缀（不需要路径转换）
		return baseURL + e.PathPrefix
	default:
		// 向后兼容：默认使用 anthropic 格式，需要添加 /v1 前缀
		return baseURL + "/v1" + path
	}
}

// 优化 IsAvailable 方法，减少锁的持有时间
func (e *Endpoint) IsAvailable() bool {
	e.mutex.RLock()
	enabled := e.Enabled
	status := e.Status
	e.mutex.RUnlock()

	return enabled && status == StatusActive
}

func (e *Endpoint) RecordRequest(success bool, requestID string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := time.Now()

	// 添加到环形缓冲区（包含请求ID）
	record := utils.RequestRecord{
		Timestamp: now,
		Success:   success,
		RequestID: requestID,
	}
	e.RequestHistory.Add(record)

	e.TotalRequests++
	if success {
		e.SuccessRequests++
		e.FailureCount = 0      // 重置失败计数
		e.SuccessiveSuccesses++ // 增加连续成功次数
		// 如果成功且之前是不可用状态，恢复为可用
		if e.Status == StatusInactive {
			// 释放 mutex 以避免死锁，因为 MarkActive 需要获取 mutex
			e.mutex.Unlock()
			e.MarkActive()
			e.mutex.Lock()
		}
	} else {
		e.FailureCount++
		e.LastFailure = now
		e.SuccessiveSuccesses = 0 // 重置连续成功次数

		// 使用环形缓冲区检查是否应该标记为不可用
		if e.Status == StatusActive && e.RequestHistory.ShouldMarkInactive(now) {
			// 释放 mutex 以避免死锁，因为 MarkInactiveWithReason 需要获取 mutex
			e.mutex.Unlock()
			e.MarkInactiveWithReason()
			e.mutex.Lock()
		}
	}
}

func (e *Endpoint) MarkInactive() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.Status = StatusInactive
}

// MarkInactiveWithReason 标记端点为失效并记录原因
func (e *Endpoint) MarkInactiveWithReason() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.Status == StatusActive {
		e.Status = StatusInactive

		// 从循环缓冲区获取导致失效的请求ID
		failedRequestIDs := e.RequestHistory.GetRecentFailureRequestIDs(time.Now())

		// 构建失效原因记录
		e.blacklistMutex.Lock()
		e.BlacklistReason = &BlacklistReason{
			BlacklistedAt:     time.Now(),
			CausingRequestIDs: failedRequestIDs,
			ErrorSummary:      fmt.Sprintf("Endpoint failed due to %d consecutive failures", len(failedRequestIDs)),
		}
		e.blacklistMutex.Unlock()
	}
}

func (e *Endpoint) MarkActive() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.Status = StatusActive
	e.FailureCount = 0
	e.SuccessiveSuccesses = 0 // 重置连续成功次数

	// 清除失效原因记录
	e.blacklistMutex.Lock()
	e.BlacklistReason = nil
	e.blacklistMutex.Unlock()

	// 重置跳过健康检查日志时间，确保下次rate limit时能立即记录
	e.lastSkipLogTime = time.Time{}

	// 清理历史记录
	e.RequestHistory.Clear()
}

func (e *Endpoint) GetSuccessiveSuccesses() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.SuccessiveSuccesses
}

func generateID(name string) string {
	// Use stable ID based on endpoint name hash for statistics persistence
	return statistics.GenerateEndpointID(name)
}

// parseDuration 解析时间字符串，失败时返回默认值
func parseDuration(durationStr string, defaultDuration time.Duration) time.Duration {
	if durationStr == "" {
		return defaultDuration
	}
	if duration, err := time.ParseDuration(durationStr); err == nil {
		return duration
	}
	return defaultDuration
}

// CreateProxyClient 为这个端点创建支持代理的HTTP客户端
func (e *Endpoint) CreateProxyClient(timeoutConfig config.ProxyTimeoutConfig) (*http.Client, error) {
	e.mutex.RLock()
	proxyConfig := e.Proxy
	e.mutex.RUnlock()

	factory := httpclient.NewFactory()
	clientConfig := httpclient.ClientConfig{
		Type: httpclient.ClientTypeEndpoint,
		Timeouts: httpclient.TimeoutConfig{
			TLSHandshake:   parseDuration(timeoutConfig.TLSHandshake, 10*time.Second),
			ResponseHeader: parseDuration(timeoutConfig.ResponseHeader, 60*time.Second),
			IdleConnection: parseDuration(timeoutConfig.IdleConnection, 90*time.Second),
			OverallRequest: parseDuration(timeoutConfig.OverallRequest, 0),
		},
		ProxyConfig: proxyConfig,
	}

	return factory.CreateClient(clientConfig)
}

// CreateHealthClient 为健康检查创建HTTP客户端（使用与代理相同的配置，但超时较短）
func (e *Endpoint) CreateHealthClient(timeoutConfig config.HealthCheckTimeoutConfig) (*http.Client, error) {
	e.mutex.RLock()
	proxyConfig := e.Proxy
	e.mutex.RUnlock()

	factory := httpclient.NewFactory()
	clientConfig := httpclient.ClientConfig{
		Type: httpclient.ClientTypeHealth,
		Timeouts: httpclient.TimeoutConfig{
			TLSHandshake:   parseDuration(timeoutConfig.TLSHandshake, 5*time.Second),
			ResponseHeader: parseDuration(timeoutConfig.ResponseHeader, 30*time.Second),
			IdleConnection: parseDuration(timeoutConfig.IdleConnection, 60*time.Second),
			OverallRequest: parseDuration(timeoutConfig.OverallRequest, 30*time.Second),
		},
		ProxyConfig: proxyConfig,
	}

	return factory.CreateClient(clientConfig)
}

// RefreshOAuthToken 刷新 OAuth token
func (e *Endpoint) RefreshOAuthToken(timeoutConfig config.ProxyTimeoutConfig) error {
	return e.RefreshOAuthTokenWithCallback(timeoutConfig, nil)
}

// RefreshOAuthTokenWithCallback 刷新 OAuth token 并可选地调用回调函数
func (e *Endpoint) RefreshOAuthTokenWithCallback(timeoutConfig config.ProxyTimeoutConfig, onTokenRefreshed func(*Endpoint) error) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.AuthType != "oauth" {
		return fmt.Errorf("endpoint is not configured for oauth authentication")
	}

	if e.OAuthConfig == nil {
		return fmt.Errorf("oauth config is nil")
	}

	// 创建HTTP客户端用于刷新请求
	factory := httpclient.NewFactory()
	clientConfig := httpclient.ClientConfig{
		Type: httpclient.ClientTypeProxy,
		Timeouts: httpclient.TimeoutConfig{
			TLSHandshake:   parseDuration(timeoutConfig.TLSHandshake, 10*time.Second),
			ResponseHeader: parseDuration(timeoutConfig.ResponseHeader, 60*time.Second),
			IdleConnection: parseDuration(timeoutConfig.IdleConnection, 90*time.Second),
			OverallRequest: parseDuration(timeoutConfig.OverallRequest, 30*time.Second),
		},
		ProxyConfig: e.Proxy,
	}

	client, err := factory.CreateClient(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create http client for token refresh: %v", err)
	}

	// 刷新token
	newOAuthConfig, err := oauth.RefreshToken(e.OAuthConfig, client)
	if err != nil {
		return fmt.Errorf("failed to refresh oauth token: %v", err)
	}

	// 更新配置
	e.OAuthConfig = newOAuthConfig

	// 如果提供了回调函数，调用它来处理配置持久化
	if onTokenRefreshed != nil {
		if err := onTokenRefreshed(e); err != nil {
			// 回调失败，但token已经刷新成功，只记录错误
			return fmt.Errorf("oauth token refreshed successfully but failed to persist to config file: %v", err)
		}
	}

	return nil
}

// GetAuthHeaderWithRefresh 获取认证头部，如果需要会自动刷新OAuth token
func (e *Endpoint) GetAuthHeaderWithRefresh(timeoutConfig config.ProxyTimeoutConfig) (string, error) {
	return e.GetAuthHeaderWithRefreshCallback(timeoutConfig, nil)
}

// GetAuthHeaderWithRefreshCallback 获取认证头部，如果需要会自动刷新OAuth token，支持回调
func (e *Endpoint) GetAuthHeaderWithRefreshCallback(timeoutConfig config.ProxyTimeoutConfig, onTokenRefreshed func(*Endpoint) error) (string, error) {
	// 首先尝试获取认证头部
	authHeader, err := e.GetAuthHeader()

	if e.AuthType == "oauth" {
		if err != nil {
			// 如果获取失败且token确实过期，尝试刷新
			if oauth.IsTokenExpired(e.OAuthConfig) {
				if refreshErr := e.RefreshOAuthTokenWithCallback(timeoutConfig, onTokenRefreshed); refreshErr != nil {
					return "", fmt.Errorf("failed to refresh oauth token: %v", refreshErr)
				}
				// 重新获取认证头部
				return e.GetAuthHeader()
			}
			// 如果不是因为过期导致的错误，直接返回错误
			return "", err
		}

		// 即使获取成功，也检查是否应该主动刷新
		if oauth.ShouldRefreshToken(e.OAuthConfig) {
			// 主动刷新，但如果失败不影响当前请求
			if refreshErr := e.RefreshOAuthTokenWithCallback(timeoutConfig, onTokenRefreshed); refreshErr != nil {
				// 刷新失败，记录日志但继续使用当前token
				// 这里可以添加日志记录
			} else {
				// 刷新成功，获取新的认证头部
				if newAuthHeader, newErr := e.GetAuthHeader(); newErr == nil {
					return newAuthHeader, nil
				}
			}
		}
	}

	return authHeader, err
}

// GetBlacklistReason 安全地获取被拉黑原因信息
func (e *Endpoint) GetBlacklistReason() *BlacklistReason {
	e.blacklistMutex.RLock()
	defer e.blacklistMutex.RUnlock()

	if e.BlacklistReason == nil {
		return nil
	}

	// 返回深度拷贝以避免并发修改
	return &BlacklistReason{
		CausingRequestIDs: append([]string{}, e.BlacklistReason.CausingRequestIDs...),
		BlacklistedAt:     e.BlacklistReason.BlacklistedAt,
		ErrorSummary:      e.BlacklistReason.ErrorSummary,
	}
}

// UpdateRateLimitState 更新endpoint的rate limit状态（线程安全）
func (e *Endpoint) UpdateRateLimitState(reset *int64, status *string) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 检查是否有变化
	changed := false

	// 比较reset值
	if (e.RateLimitReset == nil) != (reset == nil) {
		changed = true
	} else if e.RateLimitReset != nil && reset != nil && *e.RateLimitReset != *reset {
		changed = true
	}

	// 比较status值
	if (e.RateLimitStatus == nil) != (status == nil) {
		changed = true
	} else if e.RateLimitStatus != nil && status != nil && *e.RateLimitStatus != *status {
		changed = true
	}

	// 如果有变化，更新状态
	if changed {
		e.RateLimitReset = reset
		e.RateLimitStatus = status
	}

	return changed, nil
}

// GetRateLimitState 安全地获取rate limit状态
func (e *Endpoint) GetRateLimitState() (*int64, *string) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	var reset *int64
	var status *string

	if e.RateLimitReset != nil {
		resetCopy := *e.RateLimitReset
		reset = &resetCopy
	}

	if e.RateLimitStatus != nil {
		statusCopy := *e.RateLimitStatus
		status = &statusCopy
	}

	return reset, status
}

// IsAnthropicEndpoint 检查是否为api.anthropic.com端点
func (e *Endpoint) IsAnthropicEndpoint() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return strings.Contains(strings.ToLower(e.URL), "api.anthropic.com")
}

// ShouldMonitorRateLimit 检查是否应该监控此端点的rate limit
func (e *Endpoint) ShouldMonitorRateLimit() bool {
	return e.IsAnthropicEndpoint()
}

// ShouldSkipHealthCheckUntilReset 检查是否应跳过健康检查直到rate limit reset时间
func (e *Endpoint) ShouldSkipHealthCheckUntilReset() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	// 1. 必须是Anthropic官方端点
	if !strings.Contains(strings.ToLower(e.URL), "api.anthropic.com") {
		return false
	}

	// 2. 必须有rate limit reset信息
	if e.RateLimitReset == nil {
		return false
	}

	// 3. 当前时间必须小于reset时间
	currentTime := time.Now().Unix()
	return currentTime < *e.RateLimitReset
}

// GetRateLimitResetTimeRemaining 获取距离rate limit reset还有多长时间（秒）
func (e *Endpoint) GetRateLimitResetTimeRemaining() int64 {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.RateLimitReset == nil {
		return 0
	}

	currentTime := time.Now().Unix()
	remaining := *e.RateLimitReset - currentTime
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ShouldLogSkipHealthCheck 判断是否应该记录跳过健康检查的日志
// 策略：首次跳过时记录，然后每5分钟记录一次，避免日志过多
func (e *Endpoint) ShouldLogSkipHealthCheck() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := time.Now()
	// 如果从未记录过，或者距离上次记录超过5分钟，则应该记录
	if e.lastSkipLogTime.IsZero() || now.Sub(e.lastSkipLogTime) >= 5*time.Minute {
		e.lastSkipLogTime = now
		return true
	}
	return false
}

// ShouldDisableOnAllowedWarning 检查是否应该在allowed_warning状态下禁用端点
// 只有同时满足以下条件时才返回true：
// 1. 启用了增强保护 (EnhancedProtection = true)
// 2. 是Anthropic官方端点 (api.anthropic.com)
// 3. 当前rate limit状态为allowed_warning
func (e *Endpoint) ShouldDisableOnAllowedWarning() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	// 必须启用增强保护
	if !e.EnhancedProtection {
		return false
	}

	// 必须是Anthropic官方端点
	if !strings.Contains(strings.ToLower(e.URL), "api.anthropic.com") {
		return false
	}

	// 必须有rate limit status信息且为allowed_warning
	if e.RateLimitStatus == nil || *e.RateLimitStatus != "allowed_warning" {
		return false
	}

	return true
}
