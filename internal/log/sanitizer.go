package log

import (
	"regexp"
	"strings"
)

// ─── 脱敏规则 ─────────────────────────────────────────────

// SanitizeRule 定义一条脱敏规则。
type SanitizeRule struct {
	Pattern *regexp.Regexp
	Replace string
}

// RuleOption 用于配置脱敏规则。
type RuleOption func(*SanitizeRule)

// ─── 脱敏器 ───────────────────────────────────────────────

// Sanitizer 对日志文本进行脱敏处理。
type Sanitizer struct {
	rules        []SanitizeRule
	semanticMode bool // 语义模式：使用描述性替换文本，便于 LLM 理解被脱敏内容的类型
}

// NewSanitizer 创建脱敏器，使用内置的默认脱敏规则。
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		rules: defaultRules(false),
	}
}

// NewSanitizerWithSemantic 创建脱敏器，使用语义化替换文本。
// 语义模式将脱敏标记替换为更描述性的文本（如 "***JWT_TOKEN_REDACTED***"），
// 使 LLM 能够理解被脱敏内容的类型，提升诊断分析的准确性。
func NewSanitizerWithSemantic() *Sanitizer {
	return &Sanitizer{
		rules:        defaultRules(true),
		semanticMode: true,
	}
}

// NewSanitizerWithRules 创建脱敏器并指定自定义规则。
func NewSanitizerWithRules(rules []SanitizeRule) *Sanitizer {
	return &Sanitizer{rules: rules}
}

// IsSemantic returns true if the sanitizer is in semantic mode.
func (s *Sanitizer) IsSemantic() bool {
	return s.semanticMode
}

// Sanitize 对输入文本执行所有脱敏规则，返回脱敏后的文本。
//
// 脱敏策略：保守脱敏——宁可漏脱敏不可误脱敏。
// 高优先级规则先执行，避免低优先级规则破坏高优先级匹配。
func (s *Sanitizer) Sanitize(input string) string {
	if input == "" {
		return ""
	}

	result := input
	for _, rule := range s.rules {
		result = rule.Pattern.ReplaceAllString(result, rule.Replace)
	}
	return result
}

// SanitizeLines 对每行分别脱敏，返回脱敏后的行切片。
func (s *Sanitizer) SanitizeLines(lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = s.Sanitize(line)
	}
	return result
}

// AddRule 动态添加脱敏规则。
func (s *Sanitizer) AddRule(pattern *regexp.Regexp, replace string) {
	s.rules = append(s.rules, SanitizeRule{
		Pattern: pattern,
		Replace: replace,
	})
}

// defaultRules 返回内置的默认脱敏规则。
//
// 当 semantic=true 时使用描述性替换文本，便于 LLM 分析被脱敏内容的类型。
// 优先级由高到低排列：
//
//	1. JWT Token (Bearer eyJ...)
//	2. AWS Access Key (AKIA...)
//	3. Basic Auth URL 中的凭证
//	4. SSH/RSA/EC Private Key
//	5. 高熵字符串兜底（长度 >= 40）
func defaultRules(semantic bool) []SanitizeRule {
	if semantic {
		return semanticRules()
	}
	return standardRules()
}

// standardRules 返回标准脱敏规则（简短标记）。
func standardRules() []SanitizeRule {
	return []SanitizeRule{
		{
			Pattern: regexp.MustCompile(`Bearer\s+eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),
			Replace: "Bearer ***JWT***",
		},
		{
			Pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
			Replace: "***AWS_KEY***",
		},
		{
			Pattern: regexp.MustCompile(`(?:https?://)[^:]+:[^@]+@`),
			Replace: "***CREDENTIALS@",
		},
		{
			Pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
			Replace: "***PRIVATE_KEY***",
		},
		{
			Pattern: regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`),
			Replace: "***GH_TOKEN***",
		},
		{
			Pattern: regexp.MustCompile(`"auth":"[^"]{10,}"`),
			Replace: `"auth":"***DOCKER_AUTH***"`,
		},
		{
			Pattern: regexp.MustCompile(`_authToken=[a-zA-Z0-9\-_]{20,}`),
			Replace: "_authToken=***NPM_TOKEN***",
		},
		{
			Pattern: regexp.MustCompile(`[a-zA-Z0-9_\-]{40,}`),
			Replace: "***HIGH_ENTROPY***",
		},
	}
}

// semanticRules 返回语义化脱敏规则（描述性替换文本）。
// 使用 "***TYPE_REDACTED***" 格式，帮助 LLM 理解被脱敏内容的类型。
func semanticRules() []SanitizeRule {
	return []SanitizeRule{
		{
			Pattern: regexp.MustCompile(`Bearer\s+eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),
			Replace: "Bearer ***JWT_TOKEN_REDACTED***",
		},
		{
			Pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
			Replace: "***AWS_ACCESS_KEY_REDACTED***",
		},
		{
			Pattern: regexp.MustCompile(`(?:https?://)[^:]+:[^@]+@`),
			Replace: "***CREDENTIALS_REDACTED@",
		},
		{
			Pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
			Replace: "***PRIVATE_KEY_REDACTED***",
		},
		{
			Pattern: regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`),
			Replace: "***GITHUB_TOKEN_REDACTED***",
		},
		{
			Pattern: regexp.MustCompile(`"auth":"[^"]{10,}"`),
			Replace: `"auth":"***DOCKER_AUTH_REDACTED***"`,
		},
		{
			Pattern: regexp.MustCompile(`_authToken=[a-zA-Z0-9\-_]{20,}`),
			Replace: "_authToken=***NPM_TOKEN_REDACTED***",
		},
		{
			Pattern: regexp.MustCompile(`[a-zA-Z0-9_\-]{40,}`),
			Replace: "***HIGH_ENTROPY_STRING_REDACTED***",
		},
	}
}

// ─── 便捷方法 ─────────────────────────────────────────────

// SanitizeString 使用默认脱敏器快速脱敏。
func SanitizeString(input string) string {
	return NewSanitizer().Sanitize(input)
}

// MustCompilePattern 编译正则表达式，编译失败会 panic。
// 用于在 init() 或包级别定义脱敏规则。
func MustCompilePattern(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

// MatchAny 检查输入是否匹配任一信号词。
func MatchAny(input string, signals []string) bool {
	if input == "" || len(signals) == 0 {
		return false
	}
	inputLower := strings.ToLower(input)
	for _, signal := range signals {
		if strings.Contains(inputLower, strings.ToLower(signal)) {
			return true
		}
	}
	return false
}

// MatchAnyRegex 检查输入是否匹配任一正则模式。
func MatchAnyRegex(input string, patterns []*regexp.Regexp) bool {
	if input == "" || len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if p.MatchString(input) {
			return true
		}
	}
	return false
}
