// Package pipeline 提供对外暴露的 DAG 模型定义。
//
// 这些类型被 CLI、API 和外部工具共用，不应引用 internal 包。
package pipeline

// PipelineSpec 是从 JSON 文件反序列化的流水线定义。
// 这是 miniflow CLI 主要的输入格式。
type PipelineSpec struct {
	Version   string     `json:"version"`             // Schema 版本，当前 "1.0"
	Name      string     `json:"name"`                // 流水线名称
	Workspace string     `json:"workspace,omitempty"` // 共享工作空间路径（容器内视角）
	Steps     []StepSpec `json:"steps"`               // 步骤列表（串行执行）
}

// StepSpec 定义流水线中的一个步骤。
type StepSpec struct {
	Name      string   `json:"name"`                 // 步骤唯一名称
	Image     string   `json:"image"`                // 容器镜像（如 "golang:1.22"）
	Commands  []string `json:"commands"`             // 按顺序执行的命令
	DependsOn []string `json:"depends_on,omitempty"` // 依赖的步骤名称列表
	Cache     *Cache   `json:"cache,omitempty"`      // 缓存挂载策略
	Env       []string `json:"env,omitempty"`        // 环境变量（K=V 格式）
	Timeout   int      `json:"timeout,omitempty"`    // 步骤超时时间（秒），0 表示不限制
}

// Cache 定义缓存挂载策略。
type Cache struct {
	Path string `json:"path"` // 容器内需要缓存挂载的路径
	Key  string `json:"key"`  // 缓存键（支持 {{ checksum "file" }} 模板）
}

// Validate 对 PipelineSpec 进行基础合法性检查。
func (s *PipelineSpec) Validate() error {
	if s.Name == "" {
		return ErrEmptyName
	}
	if s.Version == "" {
		s.Version = "1.0"
	}
	if len(s.Steps) == 0 {
		return ErrNoSteps
	}
	for i, step := range s.Steps {
		if step.Name == "" {
			return ErrStepNameEmpty(i)
		}
		if step.Image == "" {
			return ErrStepImageEmpty(step.Name)
		}
		if len(step.Commands) == 0 {
			return ErrStepNoCommands(step.Name)
		}
	}
	return nil
}
