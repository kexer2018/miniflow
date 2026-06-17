package log

import (
	"regexp"
	"strings"

	"github.com/kexer2018/miniflow/internal/pipeline"
)

// ─── 信号分类规则 ─────────────────────────────────────────

// Classifier 是确定性的日志分类器。
//
// 根据文档 3.3 节的规则，基于信号词进行分流：
//   - app_error  → 应用代码错误（只读解释）
//   - infra_error → 基础设施错误（触发修复流）
//   - unknown    → 兜底
type Classifier struct {
	appRules  []classificationRule
	infraRules []classificationRule
}

type classificationRule struct {
	Name     string
	Signals  []string         // 信号词（任一个匹配即可）
	Patterns []*regexp.Regexp // 正则模式（任一个匹配即可，优先级高于 Signals）
}

// NewClassifier 创建确定性日志分类器。
func NewClassifier() *Classifier {
	return &Classifier{
		appRules:   defaultAppRules(),
		infraRules: defaultInfraRules(),
	}
}

// Classify 对日志文本进行分类。
func (c *Classifier) Classify(logText string) pipeline.Classification {
	if logText == "" {
		return pipeline.Classification{
			Type:   pipeline.Unknown,
			Reason: "empty log",
		}
	}

	lines := strings.Split(logText, "\n")
	allSignals := make([]string, 0)

	// 优先检查 app_error 规则
	for _, rule := range c.appRules {
		if matched, signals := matchRule(logText, rule); matched {
			allSignals = append(allSignals, rule.Name)
			return pipeline.Classification{
				Type:    pipeline.AppError,
				Reason:  rule.Name,
				Signals: signals,
			}
		}
	}

	// 检查 infra_error 规则
	for _, rule := range c.infraRules {
		if matched, signals := matchRule(logText, rule); matched {
			allSignals = append(allSignals, rule.Name)
			return pipeline.Classification{
				Type:    pipeline.InfraError,
				Reason:  rule.Name,
				Signals: signals,
			}
		}
	}

	// 兜底：检查是否有 exit code 1
	if strings.Contains(logText, "exit code 1") {
		return pipeline.Classification{
			Type:    pipeline.Unknown,
			Reason:  "exit code 1 with no other signals",
			Signals: []string{"exit code 1"},
		}
	}

	_ = lines
	return pipeline.Classification{
		Type:   pipeline.Unknown,
		Reason: "no matching signals",
	}
}

// matchRule 检查日志是否匹配某条规则。
func matchRule(logText string, rule classificationRule) (bool, []string) {
	matchedSignals := make([]string, 0)
	logLower := strings.ToLower(logText)

	// 先检查正则模式（优先级高）
	for _, p := range rule.Patterns {
		if p.MatchString(logText) {
			matchedSignals = append(matchedSignals, p.String())
			return true, matchedSignals
		}
	}

	// 再检查信号词
	for _, signal := range rule.Signals {
		if strings.Contains(logLower, strings.ToLower(signal)) {
			matchedSignals = append(matchedSignals, signal)
			return true, matchedSignals
		}
	}

	return false, nil
}

// ─── 默认规则 ─────────────────────────────────────────────

// defaultAppRules 返回应用代码错误的分类规则。
func defaultAppRules() []classificationRule {
	return []classificationRule{
		{
			Name:    "panic",
			Signals: []string{"panic:"},
		},
		{
			Name:    "JavaScript/TypeScript error",
			Signals: []string{
				"syntaxerror",
				"typeerror",
				"referenceerror",
				"undefined is not a function",
				"cannot read property",
				"cannot read properties of undefined",
			},
		},
		{
			Name:    "Python exception",
			Signals: []string{"traceback (most recent call last)"},
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`Traceback \(most recent call last\)`),
			},
		},
		{
			Name: "NullPointer (Java/Kotlin)",
			Signals: []string{
				"nullpointerexception",
				"nullpointer",
				"nil pointer",
			},
		},
		{
			Name:    "Go panic/fatal",
			Signals: []string{"fatal error:", "goroutine "},
		},
	}
}

// defaultInfraRules 返回基础设施错误的分类规则。
func defaultInfraRules() []classificationRule {
	return []classificationRule{
		{
			Name: "authentication (401/403)",
			Signals: []string{
				"401 unauthorized",
				"403 forbidden",
				"authentication required",
				"authentication failed",
			},
		},
		{
			Name: "image pull failure",
			Signals: []string{
				"no such image",
				"manifest not found",
				"manifest unknown",
				"not found: manifest unknown",
			},
		},
		{
			Name: "network connectivity",
			Signals: []string{
				"connection refused",
				"dial tcp",
				"tls handshake timeout",
				"connection reset by peer",
				"no route to host",
				"i/o timeout",
			},
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`dial tcp\s+\S+:\d+`),
			},
		},
		{
			Name: "file permission",
			Signals: []string{
				"permission denied",
				"cannot open file",
				"access denied",
			},
		},
		{
			Name: "disk space",
			Signals: []string{
				"no space left on device",
				"disk quota exceeded",
				"cannot allocate memory",
			},
		},
		{
			Name: "credential/certificate",
			Signals: []string{
				"certificate has expired",
				"certificate expired",
				"x509: certificate",
				"certificate signed by unknown authority",
			},
		},
		{
			Name: "configuration not found",
			Signals: []string{
				"could not find",
				"configuration not found",
				"config file not found",
			},
			Patterns: []*regexp.Regexp{
				regexp.MustCompile(`(?i)(?:could not find|not found).*(?:config|credential|cred|secret|key|token)`),
				regexp.MustCompile(`(?i)(?:config|credential|cred|secret|key|token).*(?:not found|missing)`),
			},
		},
	}
}
