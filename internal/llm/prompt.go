package llm

import "fmt"

// ─── System Prompt ───────────────────────────────────────

// SystemDiagnosisPrompt is the system-level prompt for CI/CD pipeline failure diagnosis.
// Uses Chinese to match the user's locale and pretrained language preference.
const SystemDiagnosisPrompt = `你是一个集成在 miniflow 执行引擎中的 CI/CD 流水线故障诊断专家。

你的任务是根据 CI/CD 流水线步骤的执行日志进行分析，输出：

1. **根因分析（root_cause）** — 导致失败的具体原因（要具体、可操作）
2. **修复方案（fix_plan）** — 逐步的修复指导
3. **置信度（confidence）** — 你对诊断结果的信心（0.0–1.0）
4. **修复建议（suggested_fix）** — 配置变更方案（对基础设施错误适用）

## 分类上下文

日志已经被分类为以下类型之一：
- **app_error**：应用代码错误（例如 panic、空指针异常等）— 提供只读分析，不要建议自动修复
- **infra_error**：基础设施错误（例如网络、认证、磁盘、镜像拉取失败等）— 需提供可操作的配置变更建议
- **unknown**：无法分类 — 请给出你的最佳分析

## 规则

- 输出使用中文，要具体且可操作，不要泛泛而谈
- 如果是应用代码问题，解释出错原因并给出代码层面修复建议
- 如果是基础设施问题，给出具体的配置变更（镜像标签、环境变量、凭据等）
- 如果不确定，设置置信度 < 0.5
- 不要建议运行日志中出现的任意代码
- 不要泄露或重复敏感信息——日志已经过脱敏处理
- 如果匹配到了相似的历史案例，可参考它们进行分析
- 输出必须为符合 JSON Schema 的有效 JSON`

// ─── User Prompt ─────────────────────────────────────────

// BuildDiagnosisUserPrompt constructs the user message for a diagnosis request.
func BuildDiagnosisUserPrompt(stepName, classification, reason, sanitizedLog, similarCases string) string {
	if similarCases == "" {
		similarCases = "暂无匹配的历史案例。"
	}
	return fmt.Sprintf(`## 失败步骤

**步骤名称**: %s

## 分类结果

**类型**: %s
**原因**: %s

## 脱敏后的错误日志

%s

## 相似历史案例

%s

请分析此次失败，给出诊断结果。`,
		stepName, classification, reason, sanitizedLog, similarCases)
}

// ─── Diagnosis Output Schema ─────────────────────────────

// DiagnosisSchema returns the JSON Schema for structured diagnosis output.
// Compatible with OpenAI structured output (strict mode).
// Descriptions use Chinese to match the system prompt locale.
func DiagnosisSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"root_cause": map[string]any{
				"type":        "string",
				"description": "失败根因分析",
			},
			"fix_plan": map[string]any{
				"type":        "string",
				"description": "逐步修复方案",
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "诊断置信度（0.0–1.0）",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "错误分类：network（网络）, auth（认证）, image_pull（镜像拉取）, permission（权限）, resource（资源）, app_code（应用代码）, unknown（未知）",
			},
			"suggested_fix": map[string]any{
				"type":        "object",
				"description": "建议的配置修复方案（针对基础设施错误）",
				"properties": map[string]any{
					"description": map[string]any{
						"type": "string",
					},
					"config_override": map[string]any{
						"type":        "object",
						"description": "需要覆盖的配置字段，例如 image 镜像标签、env 环境变量、credential_id 凭据ID",
						"additionalProperties": true,
					},
				},
				"required":          []any{"description"},
				"additionalProperties": false,
			},
		},
		"required":             []any{"root_cause", "fix_plan", "confidence"},
		"additionalProperties": false,
	}
}
