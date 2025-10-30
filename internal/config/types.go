package config

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Endpoints  []EndpointConfig `yaml:"endpoints"`
	Logging    LoggingConfig    `yaml:"logging"`
	Validation ValidationConfig `yaml:"validation"`
	Tagging    TaggingConfig    `yaml:"tagging"`  // 标签系统配置（永远启用）
	Timeouts   TimeoutConfig    `yaml:"timeouts"` // 超时配置
	I18n       I18nConfig       `yaml:"i18n"`     // 国际化配置
}

// I18nConfig 国际化配置
type I18nConfig struct {
	Enabled         bool   `yaml:"enabled"`          // 是否启用国际化
	DefaultLanguage string `yaml:"default_language"` // 默认语言
	LocalesPath     string `yaml:"locales_path"`     // 语言文件路径
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type EndpointConfig struct {
	Name                       string              `yaml:"name"`
	URL                        string              `yaml:"url"`
	EndpointType               string              `yaml:"endpoint_type"`         // "anthropic" | "openai" 等
	PathPrefix                 string              `yaml:"path_prefix,omitempty"` // OpenAI端点的路径前缀，如 "/v1/chat/completions"
	AuthType                   string              `yaml:"auth_type"`
	AuthValue                  string              `yaml:"auth_value"`
	Enabled                    bool                `yaml:"enabled"`
	Priority                   int                 `yaml:"priority"`
	Tags                       []string            `yaml:"tags"`                                                                                   // 新增：支持的tag列表
	ModelRewrite               *ModelRewriteConfig `yaml:"model_rewrite,omitempty"`                                                                // 新增：模型重写配置
	Proxy                      *ProxyConfig        `yaml:"proxy,omitempty"`                                                                        // 新增：代理配置
	OAuthConfig                *OAuthConfig        `yaml:"oauth_config,omitempty"`                                                                 // 新增：OAuth配置
	HeaderOverrides            map[string]string   `yaml:"header_overrides,omitempty" json:"header_overrides,omitempty"`                           // 新增：HTTP Header覆盖配置
	ParameterOverrides         map[string]string   `yaml:"parameter_overrides,omitempty" json:"parameter_overrides,omitempty"`                     // 新增：Request Parameters覆盖配置
	MaxTokensFieldName         string              `yaml:"max_tokens_field_name,omitempty" json:"max_tokens_field_name,omitempty"`                 // max_tokens 参数名转换选项
	RateLimitReset             *int64              `yaml:"rate_limit_reset,omitempty" json:"rate_limit_reset,omitempty"`                           // Anthropic-Ratelimit-Unified-Reset
	RateLimitStatus            *string             `yaml:"rate_limit_status,omitempty" json:"rate_limit_status,omitempty"`                         // Anthropic-Ratelimit-Unified-Status
	EnhancedProtection         bool                `yaml:"enhanced_protection,omitempty" json:"enhanced_protection,omitempty"`                     // 官方帐号增强保护：allowed_warning时即禁用端点
	ReorderSystemMessagesFirst *bool               `yaml:"reorder_system_messages_first,omitempty" json:"reorder_system_messages_first,omitempty"` // 仅对OpenAI端点生效：将system消息重排到messages最前面
}

// 新增：代理配置结构
type ProxyConfig struct {
	Type     string `yaml:"type" json:"type"`                             // "http" | "socks5"
	Address  string `yaml:"address" json:"address"`                       // 代理服务器地址，如 "127.0.0.1:1080"
	Username string `yaml:"username,omitempty" json:"username,omitempty"` // 代理认证用户名（可选）
	Password string `yaml:"password,omitempty" json:"password,omitempty"` // 代理认证密码（可选）
}

// 新增：OAuth 配置结构
type OAuthConfig struct {
	AccessToken  string   `yaml:"access_token" json:"access_token"`               // 访问令牌
	RefreshToken string   `yaml:"refresh_token" json:"refresh_token"`             // 刷新令牌
	ExpiresAt    int64    `yaml:"expires_at" json:"expires_at"`                   // 过期时间戳（毫秒）
	TokenURL     string   `yaml:"token_url" json:"token_url"`                     // Token刷新URL（必填）
	ClientID     string   `yaml:"client_id,omitempty" json:"client_id,omitempty"` // 客户端ID
	Scopes       []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`       // 权限范围
	AutoRefresh  bool     `yaml:"auto_refresh" json:"auto_refresh"`               // 是否自动刷新
}

// 新增：模型重写配置结构
type ModelRewriteConfig struct {
	Enabled bool               `yaml:"enabled" json:"enabled"` // 是否启用模型重写
	Rules   []ModelRewriteRule `yaml:"rules" json:"rules"`     // 重写规则列表
}

// 新增：模型重写规则
type ModelRewriteRule struct {
	SourcePattern string `yaml:"source_pattern" json:"source_pattern"` // 源模型通配符模式
	TargetModel   string `yaml:"target_model" json:"target_model"`     // 目标模型名称
}

type LoggingConfig struct {
	Level           string `yaml:"level"`
	LogRequestTypes string `yaml:"log_request_types"`
	LogRequestBody  string `yaml:"log_request_body"`
	LogResponseBody string `yaml:"log_response_body"`
	LogDirectory    string `yaml:"log_directory"`
}

type ValidationConfig struct {
	PythonJSONFixing PythonJSONFixingConfig `yaml:"python_json_fixing"`
}

// PythonJSONFixing 配置结构
type PythonJSONFixingConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`             // 是否启用 Python JSON 修复
	TargetTools  []string `yaml:"target_tools" json:"target_tools"`   // 需要修复的工具列表
	DebugLogging bool     `yaml:"debug_logging" json:"debug_logging"` // 是否启用调试日志
	MaxAttempts  int      `yaml:"max_attempts" json:"max_attempts"`   // 最大修复尝试次数
}

// 新增：超时配置结构
type TimeoutConfig struct {
	// 网络超时设置（代理和健康检查共用）
	TLSHandshake   string `yaml:"tls_handshake" json:"tls_handshake"`     // TLS握手超时，默认10s
	ResponseHeader string `yaml:"response_header" json:"response_header"` // 响应头超时，默认60s
	IdleConnection string `yaml:"idle_connection" json:"idle_connection"` // 空闲连接超时，默认90s
	// 健康检查特有配置
	HealthCheckTimeout string `yaml:"health_check_timeout" json:"health_check_timeout"` // 健康检查整体响应超时，默认30s
	CheckInterval      string `yaml:"check_interval" json:"check_interval"`             // 健康检查间隔，默认30s
	RecoveryThreshold  int    `yaml:"recovery_threshold" json:"recovery_threshold"`     // 连续成功多少次后恢复端点，默认1
}

// 代理客户端超时配置（内部使用，从TimeoutConfig转换）
type ProxyTimeoutConfig struct {
	TLSHandshake   string `yaml:"tls_handshake" json:"tls_handshake"`
	ResponseHeader string `yaml:"response_header" json:"response_header"`
	IdleConnection string `yaml:"idle_connection" json:"idle_connection"`
	OverallRequest string `yaml:"overall_request" json:"overall_request"` // 保持为空，无限制
}

// 健康检查超时配置（内部使用，从TimeoutConfig转换）
type HealthCheckTimeoutConfig struct {
	TLSHandshake      string `yaml:"tls_handshake" json:"tls_handshake"`
	ResponseHeader    string `yaml:"response_header" json:"response_header"`
	IdleConnection    string `yaml:"idle_connection" json:"idle_connection"`
	OverallRequest    string `yaml:"overall_request" json:"overall_request"`
	CheckInterval     string `yaml:"check_interval" json:"check_interval"`
	RecoveryThreshold int    `yaml:"recovery_threshold" json:"recovery_threshold"`
}

// ToProxyTimeoutConfig 将TimeoutConfig转换为ProxyTimeoutConfig
func (tc *TimeoutConfig) ToProxyTimeoutConfig() ProxyTimeoutConfig {
	return ProxyTimeoutConfig{
		TLSHandshake:   tc.TLSHandshake,
		ResponseHeader: tc.ResponseHeader,
		IdleConnection: tc.IdleConnection,
		OverallRequest: "", // 代理不设置整体超时，支持流式响应
	}
}

// ToHealthCheckTimeoutConfig 将TimeoutConfig转换为HealthCheckTimeoutConfig
func (tc *TimeoutConfig) ToHealthCheckTimeoutConfig() HealthCheckTimeoutConfig {
	return HealthCheckTimeoutConfig{
		TLSHandshake:      tc.TLSHandshake,
		ResponseHeader:    tc.ResponseHeader,
		IdleConnection:    tc.IdleConnection,
		OverallRequest:    tc.HealthCheckTimeout,
		CheckInterval:     tc.CheckInterval,
		RecoveryThreshold: tc.RecoveryThreshold,
	}
}

// Tag系统配置结构 (永远启用)
type TaggingConfig struct {
	PipelineTimeout string         `yaml:"pipeline_timeout"`
	Taggers         []TaggerConfig `yaml:"taggers"`
}

type TaggerConfig struct {
	Name        string                 `yaml:"name"`
	Type        string                 `yaml:"type"`         // "builtin" | "starlark"
	BuiltinType string                 `yaml:"builtin_type"` // 内置类型: "path" | "header" | "body-json" | "method" | "query"
	Tag         string                 `yaml:"tag"`          // 标记的tag名称
	Enabled     bool                   `yaml:"enabled"`
	Priority    int                    `yaml:"priority"` // 执行优先级(未使用，因为并发执行)
	Config      map[string]interface{} `yaml:"config"`   // tagger特定配置
}
