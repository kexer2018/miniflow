package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/kexer2018/miniflow/internal/config"
	"github.com/kexer2018/miniflow/internal/container"
	"github.com/kexer2018/miniflow/internal/db"
	"github.com/kexer2018/miniflow/internal/fixer"
	"github.com/kexer2018/miniflow/internal/llm"
	internalLog "github.com/kexer2018/miniflow/internal/log"
	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	"github.com/kexer2018/miniflow/internal/secret"
	"github.com/kexer2018/miniflow/internal/source"
	"github.com/kexer2018/miniflow/internal/stepregistry"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

// ─── 全局标志 ─────────────────────────────────────────────

var (
	verbose         bool
	jsonInput       string
	autoDiagnose    bool
	configPath      string
	credentialsFile string
)

// ─── 已解析的全局配置 ────────────────────────────────────

var resolvedLLM config.ResolvedLLMConfig

// ─── diagnose 子命令标志 ──────────────────────────────────

var (
	diagnoseStepName string
	diagnoseLogText  string
	diagnoseLogFile  string
)

func main() {
	// 在所有命令之前加载配置文件
	cfg := loadConfig()
	resolvedLLM = config.ResolveLLMConfig(cfg)
	slog.Debug("llm config resolved",
		"model", resolvedLLM.Model,
		"base_url", resolvedLLM.BaseURL,
		"has_api_key", resolvedLLM.APIKey != "",
	)

	rootCmd := &cobra.Command{
		Use:   "miniflow",
		Short: "AI-native lightweight CI/CD execution engine",
		Long:  `miniflow — AI 原生轻量级 CI/CD 执行引擎`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			initLogger()
		},
		RunE: runPipeline,
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&autoDiagnose, "auto-diagnose", "d", false, "auto-diagnose failed steps")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "config file path (JSON)")
	rootCmd.PersistentFlags().StringVarP(&credentialsFile, "credentials", "", "", "credentials file path (JSON)")

	rootCmd.Flags().StringVarP(&jsonInput, "file", "f", "", "pipeline JSON file (required)")

	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newDiagnoseCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ─── 配置文件加载 ─────────────────────────────────────────

func loadConfig() *config.Config {
	if configPath != "" {
		cfg, err := config.LoadWithFlag(configPath)
		if err != nil {
			slog.Warn("failed to load config file", "path", configPath, "error", err)
		} else {
			return cfg
		}
	}
	return config.LoadDefault()
}

// ─── 流水线执行 ──────────────────────────────────────────

func runPipeline(cmd *cobra.Command, args []string) error {
	if jsonInput == "" {
		return fmt.Errorf("pipeline file required (use -f <file.json>)")
	}

	spec, err := readPipelineSpec(jsonInput)
	if err != nil {
		return fmt.Errorf("read pipeline: %w", err)
	}
	slog.Info("pipeline loaded", "name", spec.Name, "version", spec.Version)

	mgr, err := container.NewDockerManager()
	if err != nil {
		return fmt.Errorf("init docker: %w", err)
	}
	defer mgr.Close()

	wsManager := container.NewWorkspaceManager("")

	// 初始化持久化存储
	dbDir := filepath.Dir(container.WorkspaceBaseDir)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dbDir, "miniflow.db"))
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer store.Close()

	// ── 解析凭据 ────────────────────────────────────────
	// --credentials 标志 > 配置文件 > 默认路径 ~/.miniflow/credentials.json
	credPath := credentialsFile
	if credPath == "" {
		cfg := loadConfig()
		credPath = cfg.Credentials
	}
	if credPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			credPath = filepath.Join(home, ".miniflow", "credentials.json")
		}
	}
	credStore := secret.MustLoad(credPath)

	// ── 构建流水线 ──────────────────────────────────────
	// 将 pipeline 级 Env、step Secrets 解析合并到步骤 env 中
	p := &internalpipeline.Pipeline{
		ID:        uuid.New().String(),
		Name:      spec.Name,
		Version:   spec.Version,
		Workspace: spec.Workspace,
		Status:    internalpipeline.StatusPending,
		Steps:     buildSteps(spec, credStore),
	}

	ctx, cancel := signalContext()
	defer cancel()

	// Create workspace and clone source if configured
	wsPath, err := wsManager.CreateWorkspace(p.ID)
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	if spec.Source != nil {
		slog.Info("checking out source",
			"repo", spec.Source.Repository,
			"ref", spec.Source.Ref,
		)

		sourceMgr := source.NewManager(credStore)
		checkoutResult, err := sourceMgr.PrepareWorkspace(ctx, spec.Source, wsPath)
		if err != nil {
			return fmt.Errorf("source checkout: %w", err)
		}
		slog.Info("source checked out",
			"commit", checkoutResult.CommitSHA,
			"ref", checkoutResult.Ref,
		)
	}

	executor := internalpipeline.NewExecutor(mgr, wsManager)
	result := executor.ExecutePipeline(ctx, p)

	// 脱敏 Step 日志（用于存储和后续 LLM 分析）
	for i, sr := range result.StepResults {
		if sr.RawLog != "" {
			result.StepResults[i].Sanitized = internalLog.SanitizeString(sr.RawLog)
		}
	}

	printResult(result)

	// 自动诊断失败步骤
	if autoDiagnose && result.Status != internalpipeline.StatusSuccess {
		autoDiagnoseFailedSteps(ctx, result)
	}

	// 持久化执行结果（非致命错误仅告警）
	if err := store.SavePipelineResult(ctx, result); err != nil {
		slog.Warn("failed to save pipeline result", "error", err)
	}

	// 保存执行上下文（用于中断恢复 / 审计）
	ec := &internalpipeline.ExecContext{
		PipelineID:   p.ID,
		WorkspaceDir: wsManager.WorkspacePath(p.ID),
		CacheDir:     filepath.Join(container.WorkspaceBaseDir, ".cache"),
	}
	if err := store.SaveExecContext(ctx, ec); err != nil {
		slog.Warn("failed to save exec context", "error", err)
	}

	if result.Status != internalpipeline.StatusSuccess {
		os.Exit(1)
	}
	return nil
}

// ─── 版本命令 ─────────────────────────────────────────────

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("miniflow v0.1.0-alpha")
		},
	}
}

// ─── 验证命令 ─────────────────────────────────────────────

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate -f <file.json>",
		Short: "Validate a pipeline JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := readPipelineSpec(jsonInput)
			if err != nil {
				return err
			}
			fmt.Printf("Pipeline %q (v%s) is valid (%d steps)\n",
				spec.Name, spec.Version, len(spec.Steps))
			return nil
		},
	}
	cmd.Flags().StringVarP(&jsonInput, "file", "f", "", "pipeline JSON file (required)")
	return cmd
}

// ─── 诊断命令 ─────────────────────────────────────────────

func newDiagnoseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "AI diagnose a failed pipeline step log",
		Long: `Analyze a failed step log using AI and provide root cause analysis and fix suggestions.

Examples:
  miniflow diagnose --step build --log "error: connection refused"
  miniflow diagnose --step build --log-file ./error.log`,
		RunE: runDiagnose,
	}

	cmd.Flags().StringVar(&diagnoseStepName, "step", "", "step name (required)")
	cmd.Flags().StringVar(&diagnoseLogText, "log", "", "error log text")
	cmd.Flags().StringVar(&diagnoseLogFile, "log-file", "", "file containing error log")

	return cmd
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	if diagnoseStepName == "" {
		return fmt.Errorf("--step is required")
	}

	logText := diagnoseLogText
	if diagnoseLogFile != "" {
		data, err := os.ReadFile(diagnoseLogFile)
		if err != nil {
			return fmt.Errorf("read log file: %w", err)
		}
		logText = string(data)
	}

	if logText == "" {
		return fmt.Errorf("either --log or --log-file is required")
	}
	dc := createDiagnoseConfig()

	ctx := context.Background()
	result := fixer.Diagnose(ctx, dc, diagnoseStepName, logText)
	printDiagnosis(result)
	return nil
}

// ─── 自动诊断 ─────────────────────────────────────────────

// autoDiagnoseFailedSteps 对流水线中失败的步骤进行自动诊断。
func autoDiagnoseFailedSteps(ctx context.Context, result *internalpipeline.PipelineResult) {
	dc := createDiagnoseConfig()

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("  ✨ AI Diagnosis")
	fmt.Println("═══════════════════════════════════════════")

	for _, sr := range result.StepResults {
		if sr.Status != internalpipeline.StatusFailed {
			continue
		}

		fmt.Printf("\n  📋 Diagnosing: %s\n", sr.Name)

		logText := sr.RawLog
		if sr.Sanitized != "" {
			logText = sr.Sanitized
		}

		dx := fixer.Diagnose(ctx, dc, sr.Name, logText)
		fmt.Println()
		printDiagnosis(dx)
	}

	fmt.Println("═══════════════════════════════════════════")
}

// ─── 诊断配置 ─────────────────────────────────────────────

// createDiagnoseConfig 从已解析的配置创建诊断配置（含 LLM 客户端和种子库）。
// 即使没有 API Key，也会返回可用的 RAG-only 降级配置。
func createDiagnoseConfig() fixer.DiagnoseConfig {
	cfg := loadConfig()
	resolvedSeeds := config.ResolveSeedsConfig(cfg)

	var llmClient llm.LLMClient
	if resolvedLLM.APIKey != "" {
		llmClient = llm.NewOpenAIClientWithConfig(resolvedLLM.APIKey, resolvedLLM.BaseURL, resolvedLLM.Model)
	}

	var seedEngine *fixer.SeedEngine
	if resolvedSeeds.Enabled {
		seedEngine = fixer.NewSeedEngineWithSeedsDir(resolvedSeeds.Dir)
	} else {
		seedEngine = fixer.NewSeedEngine()
	}

	return fixer.DiagnoseConfig{
		LLM:        llmClient,
		Sanitizer:  internalLog.NewSanitizerWithSemantic(),
		Classifier: internalLog.NewClassifier(),
		SeedEngine: seedEngine,
	}
}

// ─── 输出 ─────────────────────────────────────────────────

func printDiagnosis(d *fixer.DiagnosisResult) {
	fmt.Printf("  Classification: %s", d.Classification.Type)
	if d.Classification.Reason != "" {
		fmt.Printf(" (%s)", d.Classification.Reason)
	}
	fmt.Println()

	if d.IsDegraded {
		fmt.Println("  ⚠️  Degraded mode (LLM unavailable)")
	}
	if d.Error != "" {
		fmt.Printf("  ⚠️  %s\n", d.Error)
	}

	if len(d.MatchedSeeds) > 0 {
		fmt.Printf("  Matched %d similar incidents:\n", len(d.MatchedSeeds))
		for _, m := range d.MatchedSeeds {
			fmt.Printf("    • %s [%s] (score: %.2f)\n",
				m.Seed.Title, m.Seed.Category, m.Score)
		}
	}

	fmt.Println()
	fmt.Printf("  🔍 Root Cause: %s\n", d.RootCause)
	fmt.Printf("  📝 Fix Plan:   %s\n", d.FixPlan)

	if d.SuggestedFix != nil {
		fmt.Printf("  🔧 Suggested Fix: %s\n", d.SuggestedFix.Description)
		if len(d.SuggestedFix.ConfigOverride) > 0 {
			overrideJSON, _ := json.MarshalIndent(d.SuggestedFix.ConfigOverride, "     ", "  ")
			fmt.Printf("     Config Override: %s\n", string(overrideJSON))
		}
	}

	if d.LLMUsage != nil {
		fmt.Printf("  📊 LLM tokens: %d prompt + %d completion = %d total\n",
			d.LLMUsage.PromptTokens, d.LLMUsage.CompletionTokens, d.LLMUsage.TotalTokens)
	}
}

// ─── 辅助函数 ─────────────────────────────────────────────

func initLogger() {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
}

func readPipelineSpec(path string) (*pipelinespec.PipelineSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var spec pipelinespec.PipelineSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	return &spec, nil
}

// parseEnvFile 读取并解析 .env 文件，返回 KEY=VALUE 格式列表。
// 支持：KEY=VALUE 行、# 注释、空行跳过。
func parseEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read env_file: %w", err)
	}

	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			slog.Warn("env_file: skipping line without '='", "line", line)
			continue
		}
		result = append(result, line)
	}

	slog.Debug("env_file parsed", "path", path, "pairs", len(result))
	return result, nil
}

// buildSteps converts PipelineSpec to engine Step list,
// merging pipeline-level env and resolving secrets.
func buildSteps(spec *pipelinespec.PipelineSpec, credStore *secret.CredentialStore) []internalpipeline.Step {
	// 收集 pipeline-level 环境变量（spec.Env）
	pipelineEnv := make([]string, 0, len(spec.Env))
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pipelineEnv = append(pipelineEnv, k+"="+spec.Env[k])
	}

	// 如果指定了 env_file，解析并合并（spec.Env 优先级更高）
	if spec.EnvFile != "" {
		envFilePairs, err := parseEnvFile(spec.EnvFile)
		if err != nil {
			slog.Warn("failed to read env_file, skipping", "path", spec.EnvFile, "error", err)
		} else {
			// 构建映射：env_file 先，spec.Env 覆盖
			envMap := make(map[string]string, len(envFilePairs)+len(spec.Env))
			for _, pair := range envFilePairs {
				if k, v, ok := strings.Cut(pair, "="); ok {
					envMap[k] = v
				}
			}
			for k, v := range spec.Env {
				envMap[k] = v // spec.Env 优先级更高
			}
			// 重建 pipelineEnv
			mergedKeys := make([]string, 0, len(envMap))
			for k := range envMap {
				mergedKeys = append(mergedKeys, k)
			}
			sort.Strings(mergedKeys)
			pipelineEnv = make([]string, 0, len(mergedKeys))
			for _, k := range mergedKeys {
				pipelineEnv = append(pipelineEnv, k+"="+envMap[k])
			}
		}
	}

	steps := make([]internalpipeline.Step, len(spec.Steps))
	for i, s := range spec.Steps {
		env := make([]string, len(pipelineEnv), len(pipelineEnv)+len(s.Env)+len(s.Secrets))
		copy(env, pipelineEnv)
		env = append(env, s.Env...)

		for _, secName := range s.Secrets {
			if val, ok := credStore.ResolveSecretEnv(secName); ok {
				env = append(env, val)
			} else {
				slog.Warn("secret not found, skipping",
					"secret", secName, "step", s.Name)
			}
		}

		step, err := stepregistry.Compile(s)
		if err != nil {
			slog.Warn("failed to compile step", "step", s.Name, "error", err)
		}
		step.Env = env
		step.Timeout = time.Duration(s.Timeout) * time.Second
		if s.Cache != nil {
			step.Cache = &internalpipeline.Cache{
				Path: s.Cache.Path,
				Key:  s.Cache.Key,
			}
		}
		steps[i] = step
	}
	return steps
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Warn("received signal, cancelling pipeline...")
		cancel()
	}()
	return ctx, cancel
}

func printResult(r *internalpipeline.PipelineResult) {
	// ─── 统计 ─────────────────────────────────────────
	successCount := 0
	failedCount := 0
	skippedCount := 0
	for _, sr := range r.StepResults {
		switch sr.Status {
		case internalpipeline.StatusSuccess:
			successCount++
		case internalpipeline.StatusFailed:
			failedCount++
		case internalpipeline.StatusSkipped:
			skippedCount++
		}
	}

	// ─── 顶部标题 ──────────────────────────────────────
	fmt.Println()
	printLine("╔", "╗")
	fmt.Printf("║  🚀  Pipeline: %-49s║\n", r.Name)
	printLine("╚", "╝")
	fmt.Println()

	// ─── 概要信息 ──────────────────────────────────────
	statusIcon := "✅ Success"
	switch r.Status {
	case internalpipeline.StatusSuccess:
		statusIcon = "✅  Success"
	case internalpipeline.StatusFailed:
		statusIcon = "❌  Failed"
	case internalpipeline.StatusCancelled:
		statusIcon = "🚫 Cancelled"
	}

	fmt.Printf("  Status     : %s\n", statusIcon)
	fmt.Printf("  Duration   : %d ms\n", r.DurationMs)
	fmt.Printf("  Steps      : %d total  ✅ %d  ❌ %d  ⏭️ %d\n",
		r.TotalSteps, successCount, failedCount, skippedCount)
	fmt.Println()

	// ─── 步骤明细 ──────────────────────────────────────
	fmt.Printf("  ── Steps ────────────────────────────────────\n\n")

	for _, sr := range r.StepResults {
		icon := "✅"
		statusTag := "PASS"
		switch sr.Status {
		case internalpipeline.StatusFailed:
			icon = "❌"
			statusTag = "FAIL"
		case internalpipeline.StatusSkipped:
			icon = "⏭️"
			statusTag = "SKIP"
		}

		// 步骤标题行
		stepLabel := fmt.Sprintf("%s  [%s]  %s", icon, statusTag, sr.Name)
		timeLabel := fmt.Sprintf("%d ms", sr.DurationMs)
		fmt.Printf("    %-60s %10s\n", stepLabel, timeLabel)

		// 错误信息（如果有）
		if sr.Error != "" || sr.ExitCode != 0 {
			fmt.Printf("      ── Error ─────────────────────\n")
			if sr.Error != "" {
				fmt.Printf("      %s\n", sr.Error)
			}
			if sr.ExitCode != 0 {
				fmt.Printf("      Exit code: %d\n", sr.ExitCode)
			}
			fmt.Printf("      ───────────────────────────────\n")
		}

		// 输出日志（如果有）
		if sr.RawLog != "" {
			lines := strings.Split(strings.TrimSpace(sr.RawLog), "\n")
			// 如果输出行不多，直接展示
			if len(lines) <= 12 {
				fmt.Printf("      ── Output ─────────────────────\n")
				for _, line := range lines {
					if trimmed := strings.TrimSpace(line); trimmed != "" {
						fmt.Printf("      │ %s\n", trimmed)
					}
				}
				fmt.Printf("      ───────────────────────────────\n")
			} else {
				// 输出太多，显示前 6 行 + 省略 + 后 3 行
				fmt.Printf("      ── Output (%d lines) ────────────\n", len(lines))
				for _, line := range lines[:6] {
					if trimmed := strings.TrimSpace(line); trimmed != "" {
						fmt.Printf("      │ %s\n", trimmed)
					}
				}
				fmt.Printf("      │ ... (%d more lines) ...\n", len(lines)-9)
				for _, line := range lines[len(lines)-3:] {
					if trimmed := strings.TrimSpace(line); trimmed != "" {
						fmt.Printf("      │ %s\n", trimmed)
					}
				}
				fmt.Printf("      ───────────────────────────────\n")
			}
		}
		fmt.Println()
	}

	// ─── 底部总结 ──────────────────────────────────────
	fmt.Println("  ─────────────────────────────────────────────")
	fmt.Println()

	var footerIcon string
	var footerMsg string
	switch r.Status {
	case internalpipeline.StatusSuccess:
		footerIcon = "🎉"
		footerMsg = fmt.Sprintf("All %d steps passed successfully!", r.TotalSteps)
	case internalpipeline.StatusFailed:
		footerIcon = "❌"
		footerMsg = fmt.Sprintf("Pipeline failed — %d step(s) encountered errors.", failedCount)
	case internalpipeline.StatusCancelled:
		footerIcon = "🚫"
		footerMsg = "Pipeline was cancelled."
	}

	printLine("╔", "╗")
	fmt.Printf("║  %s  %-50s║\n", footerIcon, footerMsg)
	printLine("╚", "╝")
	fmt.Println()
}

func printLine(left, right string) {
	fmt.Print(left)
	for i := 0; i < 60; i++ {
		fmt.Print("═")
	}
	fmt.Println(right)
}
