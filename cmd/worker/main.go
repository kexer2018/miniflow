package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kexer2018/miniflow/internal/api"
	"github.com/kexer2018/miniflow/internal/artifact"
	"github.com/kexer2018/miniflow/internal/container"
	"github.com/kexer2018/miniflow/internal/db"
	runservice "github.com/kexer2018/miniflow/internal/run"
	"github.com/kexer2018/miniflow/internal/secret"
	"github.com/kexer2018/miniflow/internal/stepregistry"
)

// ─── 全局标志 ─────────────────────────────────────────────

var (
	verbose            bool
	listenAddr         string
	workerID           string
	credentialsFile    string
	stepDirs           []string
	apiToken           string
	apiTokenFile       string
	workspaceRetention time.Duration
	cacheRetention     time.Duration
	artifactRetention  time.Duration
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "miniflow-worker",
		Short: "miniflow local Docker API runner",
		Long: `miniflow 本地 Docker API runner

提供本机 HTTP API，用同一个 Docker workspace 模型执行 PipelineSpec。`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			initLogger()
		},
		RunE: runWorker,
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.Flags().StringVarP(&listenAddr, "listen", "l", "127.0.0.1:9090", "worker listen address")
	rootCmd.Flags().StringVarP(&workerID, "id", "i", "", "local runner ID (default: hostname)")
	rootCmd.Flags().StringVar(&credentialsFile, "credentials", "", "credentials file path (JSON)")
	rootCmd.Flags().StringSliceVar(&stepDirs, "step-dir", nil, "trusted directory containing third-party Step bundles (repeatable)")
	rootCmd.Flags().StringVar(&apiToken, "api-token", "", "Bearer token required by the HTTP API")
	rootCmd.Flags().StringVar(&apiTokenFile, "api-token-file", "", "file containing the HTTP API Bearer token")
	rootCmd.Flags().DurationVar(&workspaceRetention, "workspace-retention", 7*24*time.Hour, "retain completed workspaces for this duration")
	rootCmd.Flags().DurationVar(&cacheRetention, "cache-retention", 30*24*time.Hour, "retain inactive caches for this duration")
	rootCmd.Flags().DurationVar(&artifactRetention, "artifact-retention", 30*24*time.Hour, "retain local artifacts for this duration")

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
	resolvedAPIToken, err := resolveAPIToken(apiToken, apiTokenFile)
	if err != nil {
		return err
	}
	if !isLoopbackListen(listenAddr) && resolvedAPIToken == "" {
		return fmt.Errorf("--listen %q is not loopback; configure --api-token or --api-token-file", listenAddr)
	}
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
	healthCtx, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := mgr.HealthCheck(healthCtx); err != nil {
		healthCancel()
		return fmt.Errorf("docker health check failed: %w", err)
	}
	healthCancel()

	// 初始化工作空间管理器
	wsManager := container.NewWorkspaceManager("")
	registry, err := stepregistry.New(stepDirs...)
	if err != nil {
		return fmt.Errorf("load Step registry: %w", err)
	}
	slog.Info("Step registry loaded", "types", len(registry.List()), "extension_dirs", len(stepDirs))

	dbDir := filepath.Dir(container.WorkspaceBaseDir)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dbDir, "miniflow.db"))
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer store.Close()

	runSvc := runservice.NewService(store, mgr, wsManager)
	runSvc.SetCredentialStore(secret.MustLoad(resolveCredentialsPath(credentialsFile)))
	runSvc.SetStepRegistry(registry)
	if err := pruneLocalStorage(context.Background(), wsManager, runSvc.ArtifactManager()); err != nil {
		slog.Warn("initial local storage cleanup failed", "error", err)
	}
	handler := api.NewHandler(store)
	handler.SetRunService(runSvc)
	handler.SetDockerHealthChecker(mgr)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           api.NewRouterWithOptions(handler, api.RouterOptions{BearerToken: resolvedAPIToken}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Advanced distributed scheduling and image preheat queues are intentionally
	// deferred until the single-node Docker product loop is stable.

	slog.Info("worker ready", "listen", listenAddr)
	slog.Info("press Ctrl+C to stop")

	// 等待退出信号
	ctx, cancel := signalContext()
	defer cancel()
	go runStoragePruner(ctx, wsManager, runSvc.ArtifactManager())

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http api listening", "addr", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("serve api: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown api: %w", err)
	}
	if err := <-errCh; err != nil {
		return fmt.Errorf("serve api: %w", err)
	}

	slog.Info("worker stopped")
	return nil
}

func runStoragePruner(ctx context.Context, workspace *container.WorkspaceManager, artifacts *artifact.Manager) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pruneLocalStorage(ctx, workspace, artifacts); err != nil {
				slog.Warn("scheduled local storage cleanup failed", "error", err)
			}
		}
	}
}

func pruneLocalStorage(ctx context.Context, workspace *container.WorkspaceManager, artifacts *artifact.Manager) error {
	now := time.Now()
	workspaces, err := workspace.PruneWorkspacesOlderThan(now.Add(-workspaceRetention))
	if err != nil {
		return fmt.Errorf("prune workspaces: %w", err)
	}
	caches, err := workspace.PruneCachesOlderThan(now.Add(-cacheRetention))
	if err != nil {
		return fmt.Errorf("prune caches: %w", err)
	}
	artifactCount, err := artifacts.PruneOlderThan(ctx, now.Add(-artifactRetention))
	if err != nil {
		return fmt.Errorf("prune artifacts: %w", err)
	}
	if workspaces+caches+artifactCount > 0 {
		slog.Info("local storage cleanup completed", "workspaces", workspaces, "caches", caches, "artifacts", artifactCount)
	}
	return nil
}

func resolveAPIToken(flagToken, tokenFile string) (string, error) {
	if flagToken != "" && tokenFile != "" {
		return "", fmt.Errorf("only one of --api-token and --api-token-file may be set")
	}
	if tokenFile == "" {
		return flagToken, nil
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("read api token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("api token file is empty")
	}
	return token, nil
}

func isLoopbackListen(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveCredentialsPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".miniflow", "credentials.json")
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
