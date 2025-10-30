package conversion

// EndpointInfo 包含转换器需要的端点信息
type EndpointInfo struct {
	Type                       string
	MaxTokensFieldName         string
	ReorderSystemMessagesFirst bool
}

// Converter 定义转换器接口
type Converter interface {
	// 转换请求
	ConvertRequest(anthropicReq []byte, endpointInfo *EndpointInfo) ([]byte, *ConversionContext, error)

	// 转换响应
	ConvertResponse(openaiResp []byte, ctx *ConversionContext, isStreaming bool) ([]byte, error)

	// 检查是否需要转换
	ShouldConvert(endpointType string) bool
}

// ConversionContext 转换上下文
type ConversionContext struct {
	EndpointType   string            // "anthropic" | "openai"
	ToolCallIDMap  map[string]string // 工具调用ID映射 (Anthropic ID -> OpenAI ID)
	IsStreaming    bool              // 是否为流式请求
	RequestHeaders map[string]string // 原始请求头
	StopSequences  []string          // 请求中的停止序列，用于响应时检测
	// 注意：不包含模型映射，因为转换发生在模型重写之后
}

// ConversionError 转换错误
type ConversionError struct {
	Type    string // "parse_error", "unsupported_feature", "tool_conversion_error"
	Message string
	Err     error
}

func (e *ConversionError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *ConversionError) Unwrap() error {
	return e.Err
}

// NewConversionError 创建转换错误
func NewConversionError(errorType, message string, err error) *ConversionError {
	return &ConversionError{
		Type:    errorType,
		Message: message,
		Err:     err,
	}
}
