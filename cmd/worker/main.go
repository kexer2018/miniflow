package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/kexer2018/miniflow/internal/container"
)

// ─── 全局标志 ─────────────────────────────────────────────

var (
	verbose    bool
	listenAddr string
	workerID   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "miniflow-worker",
		Short: "miniflow worker daemon",
		Long: `miniflow worker 守护进程

接收控制面下发的 JSON 任务，调度临时容器执行并返回结果。`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			initLogger()
		},
		RunE: runWorker,
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.Flags().StringVarP(&listenAddr, "listen", "l", ":9090", "worker listen address")
	rootCmd.Flags().StringVarP(&workerID, "id", "i", "", "worker node ID (default: hostname)")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("miniflow-worker v0.1.0-alpha")
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runWorker(cmd *cobra.Command, args []string) error {
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname
	}

	slog.Info("starting miniflow worker",
		"id", workerID,
		"listen", listenAddr,
	)

	// 初始化 Docker 管理器
	mgr, err := container.NewDockerManager()
	if err != nil {
		return fmt.Errorf("init docker: %w", err)
	}
	defer mgr.Close()

	// 初始化工作空间管理器
	wsManager := container.NewWorkspaceManager("")
	_ = wsManager

	// TODO: Phase 2 - 启动 gRPC/REST 服务监听控制面任务
	// TODO: Phase 2 - 实现镜像预热队列
	// TODO: Phase 2 - 实现任务队列与并发限制

	slog.Info("worker ready (Phase 1: CLI-only mode)")
	slog.Info("worker will serve as container execution backend for CLI")
	slog.Info("press Ctrl+C to stop")

	// 等待退出信号
	ctx, cancel := signalContext()
	defer cancel()
	<-ctx.Done()

	slog.Info("worker stopped")
	return nil
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

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Warn("received signal, shutting down...")
		cancel()
	}()
	return ctx, cancel
}
