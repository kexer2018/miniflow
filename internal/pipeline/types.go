// Package pipeline 定义 miniflow 的 DAG 核心模型与执行逻辑。
package pipeline

import (
	"time"
)

// ─── 核心类型 ──────────────────────────────────────────────

// Pipeline 表示一次完整的流水线执行实例。
type Pipeline struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Workspace string    `json:"workspace"`
	Steps     []Step    `json:"steps"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    Status    `json:"status"` // 整体运行状态
}

// Step 表示流水线中的一个执行步骤。
type Step struct {
	Name           string        `json:"name"`
	Image          string        `json:"image"`
	Commands       []string      `json:"commands"`
	DependsOn      []string      `json:"depends_on,omitempty"`
	Cache          *Cache        `json:"cache,omitempty"`
	Env            []string      `json:"env,omitempty"`
	Entrypoint     []string      `json:"entrypoint,omitempty"`
	Timeout        time.Duration `json:"-"` // 步骤超时，0 表示不限制（从 spec 的秒数转换）
	SSHAgent       bool          `json:"-"` // 是否转发宿主 SSH Agent 到容器
	NetworkEnabled bool          `json:"-"` // Whether the user Step may access the network.
	Operation      *Operation    `json:"-"` // 由受控主机操作层执行的基础 Step
	Extension      *Extension    `json:"-"` // 由受信任 Bundle 提供的脚本 Step
}

// Extension is a runner-controlled, read-only script bundle mount.
type Extension struct {
	Source string
	Target string
}

// Operation describes a non-user-script platform primitive executed by the
// local runner, such as source checkout or artifact persistence.
type Operation struct {
	Type string
	With map[string]any
}

// Cache 定义步骤级别的缓存挂载策略。
type Cache struct {
	Path string `json:"path"`
	Key  string `json:"key"`
}

// Status 表示流水线或步骤的运行状态。
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
	StatusCancelled Status = "cancelled"
)

// ─── 执行结果 ──────────────────────────────────────────────

// StepResult 记录一个 Step 的执行结果。
type StepResult struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`        // success / failed / skipped
	ExitCode   int    `json:"exit_code"`     // 容器退出码，0 表示成功
	RawLog     string `json:"-"`             // 原始日志仅在当前进程执行期间使用，绝不对外或持久化。
	Sanitized  string `json:"sanitized_log"` // 脱敏后日志（用于 LLM 分析）
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"` // 非容器退出导致的错误（如创建容器失败）
}

// PipelineResult 记录整个流水线的执行结果。
type PipelineResult struct {
	PipelineID  string       `json:"pipeline_id"`
	Name        string       `json:"name"`
	Status      Status       `json:"status"`
	TotalSteps  int          `json:"total_steps"`
	StepResults []StepResult `json:"step_results"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	DurationMs  int64        `json:"duration_ms"`
}

// ─── 日志分类 ──────────────────────────────────────────────

// LogType 表示日志分类类型。
type LogType string

const (
	AppError   LogType = "app_error"   // 应用代码错误（只读解释）
	InfraError LogType = "infra_error" // 基础设施错误（触发修复流）
	Unknown    LogType = "unknown"     // 无法分类（兜底）
)

// Classification 包含日志分类结果。
type Classification struct {
	Type    LogType  `json:"type"`
	Reason  string   `json:"reason"`
	Signals []string `json:"signals"` // 匹配到的信号词
}

// ─── 修复建议 ──────────────────────────────────────────────

// FixSuggestion 表示 AI 生成的修复建议。
type FixSuggestion struct {
	StepName     string   `json:"step_name"`
	RootCause    string   `json:"root_cause"`
	FixPlan      string   `json:"fix_plan"`
	Confident    float64  `json:"confident"` // 0-1 置信度
	SuggestedFix *StepFix `json:"suggested_fix,omitempty"`
}

// StepFix 描述一个具体的修复操作。
type StepFix struct {
	Description    string         `json:"description"`
	ConfigOverride map[string]any `json:"config_override"`
}

// ─── 执行上下文 ────────────────────────────────────────────

// ExecContext 在执行引擎内部传递各步骤共享的上下文。
type ExecContext struct {
	PipelineID   string
	WorkspaceDir string // 宿主机上的共享工作空间路径
	CacheDir     string // 宿主机上的持久化缓存根目录
}
