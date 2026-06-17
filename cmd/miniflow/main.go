package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/kexer2018/miniflow/internal/container"
	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

// ─── 全局标志 ─────────────────────────────────────────────

var (
	verbose   bool
	jsonInput string
)

func main() {
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
	rootCmd.Flags().StringVarP(&jsonInput, "file", "f", "", "pipeline JSON file (required)")
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newValidateCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

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

	p := &internalpipeline.Pipeline{
		ID:        uuid.New().String(),
		Name:      spec.Name,
		Version:   spec.Version,
		Workspace: spec.Workspace,
		Status:    internalpipeline.StatusPending,
		Steps:     convertSteps(spec.Steps),
	}

	executor := internalpipeline.NewExecutor(mgr, wsManager)
	ctx, cancel := signalContext()
	defer cancel()

	result := executor.ExecutePipeline(ctx, p)
	printResult(result)

	if result.Status != internalpipeline.StatusSuccess {
		os.Exit(1)
	}
	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("miniflow v0.1.0-alpha")
		},
	}
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
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
}

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

func convertSteps(specSteps []pipelinespec.StepSpec) []internalpipeline.Step {
	steps := make([]internalpipeline.Step, len(specSteps))
	for i, s := range specSteps {
		step := internalpipeline.Step{
			Name:      s.Name,
			Image:     s.Image,
			Commands:  s.Commands,
			DependsOn: s.DependsOn,
			Env:       s.Env,
		}
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
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("  Pipeline Result")
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("  Name:     %s\n", r.Name)
	fmt.Printf("  Status:   %s\n", r.Status)
	fmt.Printf("  Duration: %d ms\n", r.DurationMs)
	fmt.Println("───────────────────────────────────────────")

	for _, sr := range r.StepResults {
		icon := "✅"
		statusStr := string(sr.Status)
		if sr.Status == internalpipeline.StatusFailed {
			icon = "❌"
		} else if sr.Status == internalpipeline.StatusSkipped {
			icon = "⏭️"
		}

		fmt.Printf("  %s %s\n", icon, sr.Name)
		fmt.Printf("     Status:   %s\n", statusStr)
		fmt.Printf("     Duration: %d ms\n", sr.DurationMs)

		if sr.ExitCode != 0 {
			fmt.Printf("     Exit:     %d\n", sr.ExitCode)
		}
		if sr.Error != "" {
			fmt.Printf("     Error:    %s\n", sr.Error)
		}
		if sr.Status == internalpipeline.StatusFailed && sr.RawLog != "" {
			fmt.Println("     ─── Output ───")
			for _, line := range strings.Split(sr.RawLog, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					fmt.Printf("     | %s\n", trimmed)
				}
			}
			fmt.Println("     ─────────────")
		}
	}

	fmt.Println("═══════════════════════════════════════════")
}
