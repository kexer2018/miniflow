package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// ─── Docker socket 搜索路径 ───────────────────────────────
// 按优先级排列，自动检测常见 Docker 运行环境。
var dockerSocketCandidates = func() []string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",                           // 默认
		filepath.Join(home, ".orbstack/run/docker.sock"), // OrbStack
		filepath.Join(home, ".docker/run/docker.sock"),   // Docker Desktop (rootless)
		"/run/user/1000/docker.sock",                     // Rootless Docker
	}
	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		candidates = append([]string{dh}, candidates...)
	}
	return candidates
}()

func resolveDockerHost() string {
	for _, path := range dockerSocketCandidates {
		if strings.HasPrefix(path, "unix://") {
			sockPath := strings.TrimPrefix(path, "unix://")
			if _, err := os.Stat(sockPath); err == nil {
				slog.Debug("found Docker socket", "path", sockPath)
				return path
			}
		} else if _, err := os.Stat(path); err == nil {
			slog.Debug("found Docker socket", "path", path)
			return "unix://" + path
		}
	}
	return ""
}

// ─── 编译期接口检查 ───────────────────────────────────────
var _ Manager = (*DockerManager)(nil)

// DockerManager 是对 Docker SDK 的封装，实现 Manager 接口。
type DockerManager struct {
	cli *client.Client
}

// NewDockerManager 创建 DockerManager。
// 自动检测 OrbStack / Docker Desktop / 标准 Docker socket。
func NewDockerManager() (*DockerManager, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	if os.Getenv("DOCKER_HOST") == "" {
		if host := resolveDockerHost(); host != "" {
			opts = append([]client.Opt{client.WithHost(host)}, opts...)
		}
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	m := &DockerManager{cli: cli}

	if err := m.ping(context.Background()); err != nil {
		slog.Warn("Docker daemon not reachable (will retry at run time)", "error", err)
	}

	slog.Info("Docker manager initialized")
	return m, nil
}

// ─── 公开方法 ─────────────────────────────────────────────

// RunContainer 创建并运行容器，等待执行完毕，收集输出。
func (m *DockerManager) RunContainer(ctx context.Context, cfg Config) (Result, error) {
	slog.Debug("running container",
		"image", cfg.Image,
		"commands", cfg.Commands,
		"user", cfg.User,
	)

	if cfg.User == "" {
		cfg.User = "1000:1000"
	}

	// 1. 确保镜像存在
	if err := m.ensureImage(ctx, cfg.Image); err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("ensure image %s: %w", cfg.Image, err)
	}

	// 2. 准备容器配置：将命令通过 Shell 包装，支持多行 shell 语法
	cmd := shellWrap(cfg.Commands)
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh", "-c", "echo no commands specified"}
	}

	pidsLimit := DefaultResourceLimits.PidsLimit
	hostCfg := &container.HostConfig{
		Mounts:      buildMounts(cfg),
		AutoRemove:  false, // 手动清理以确保日志可读
		NetworkMode: networkMode(cfg.NetworkEnabled),
		Resources: container.Resources{
			Memory:    DefaultResourceLimits.MemoryBytes,
			NanoCPUs:  DefaultResourceLimits.NanoCPUs,
			PidsLimit: &pidsLimit,
		},
	}
	if cfg.Hardened {
		hostCfg.CapDrop = []string{"ALL"}
		hostCfg.SecurityOpt = []string{"no-new-privileges:true"}
	}

	containerCfg := &container.Config{
		Image:      cfg.Image,
		Cmd:        cmd,
		Entrypoint: cfg.Entrypoint,
		Env:        cfg.Env,
		User:       cfg.User,
		WorkingDir: cfg.WorkDir,
		Tty:        false,
	}

	// 3. 创建容器
	cont, err := m.cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("container create: %w", err)
	}

	slog.Debug("container created", "id", shortID(cont.ID))

	// 4. 启动容器
	if err := m.cli.ContainerStart(ctx, cont.ID, container.StartOptions{}); err != nil {
		m.cleanupContainer(ctx, cont.ID)
		return Result{ExitCode: -1}, fmt.Errorf("container start: %w", err)
	}

	slog.Debug("container started", "id", shortID(cont.ID))

	outputCh := make(chan string, 1)
	logErrCh := make(chan error, 1)
	go m.collectContainerLogs(ctx, cont.ID, cfg.LogCallback, outputCh, logErrCh)

	// 5. 等待容器退出
	statusCh, errCh := m.cli.ContainerWait(ctx, cont.ID, container.WaitConditionNotRunning)
	var exitCode int
	select {
	case err := <-errCh:
		if err != nil {
			m.cleanupContainer(ctx, cont.ID)
			return Result{ExitCode: -1}, fmt.Errorf("container wait: %w", err)
		}
	case resp := <-statusCh:
		exitCode = int(resp.StatusCode)
	}

	output := strings.TrimSpace(<-outputCh)
	if logErr := <-logErrCh; logErr != nil && ctx.Err() == nil {
		m.cleanupContainer(ctx, cont.ID)
		return Result{Output: output, ExitCode: exitCode}, fmt.Errorf("container logs: %w", logErr)
	}

	if exitCode == 0 {
		info, err := m.cli.ContainerInspect(ctx, cont.ID)
		if err == nil {
			exitCode = info.State.ExitCode
		}
	}

	slog.Debug("container finished",
		"id", shortID(cont.ID),
		"exit_code", exitCode,
		"output_size", len(output),
	)

	// 7. 手动清理容器
	m.cleanupContainer(ctx, cont.ID)

	return Result{
		Output:   output,
		ExitCode: exitCode,
	}, nil
}

func (m *DockerManager) collectContainerLogs(ctx context.Context, containerID string, callback LogCallback, outputCh chan<- string, errCh chan<- error) {
	logs, err := m.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		outputCh <- ""
		errCh <- err
		return
	}
	defer logs.Close()

	var stdout, stderr bytes.Buffer
	stdoutWriter := &callbackWriter{buffer: &stdout, callback: callback}
	stderrWriter := &callbackWriter{buffer: &stderr, callback: callback}
	if _, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, logs); err != nil {
		stdoutWriter.flush()
		stderrWriter.flush()
		outputCh <- strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		errCh <- err
		return
	}
	stdoutWriter.flush()
	stderrWriter.flush()

	outputCh <- strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	errCh <- nil
}

type callbackWriter struct {
	buffer   *bytes.Buffer
	callback LogCallback
	pending  []byte
}

func (w *callbackWriter) Write(p []byte) (int, error) {
	if w.buffer != nil {
		_, _ = w.buffer.Write(p)
	}
	if w.callback == nil {
		return len(p), nil
	}
	data := append(w.pending, p...)
	w.pending = w.pending[:0]
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			w.pending = append(w.pending, data...)
			return len(p), nil
		}
		line := data[:idx]
		line = bytes.TrimSuffix(line, []byte{'\r'})
		w.callback(string(line))
		data = data[idx+1:]
	}
}

func (w *callbackWriter) flush() {
	if w.callback != nil && len(w.pending) > 0 {
		w.callback(string(w.pending))
		w.pending = w.pending[:0]
	}
}

// PullImage 拉取容器镜像。
func (m *DockerManager) PullImage(ctx context.Context, imageRef string) error {
	slog.Info("pulling image", "image", imageRef)

	reader, err := m.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", imageRef, err)
	}
	defer reader.Close()

	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("read pull response: %w", err)
	}

	slog.Info("image pulled", "image", imageRef)
	return nil
}

// ImageExists 检查本地是否已缓存指定镜像。
func (m *DockerManager) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	_, _, err := m.cli.ImageInspectWithRaw(ctx, imageRef)
	if client.IsErrNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect image %s: %w", imageRef, err)
	}
	return true, nil
}

// Close 关闭 Docker 客户端连接。
func (m *DockerManager) Close() error {
	return m.cli.Close()
}

// HealthCheck verifies that the Docker daemon is reachable.
func (m *DockerManager) HealthCheck(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("docker manager is nil")
	}
	return m.ping(ctx)
}

// cleanupContainer 删除容器（忽略错误，容器可能已被删除）。
func (m *DockerManager) cleanupContainer(ctx context.Context, containerID string) {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	if err := m.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	}); err != nil {
		slog.Debug("container cleanup", "id", shortID(containerID), "error", err)
	}
}

// ─── 内部方法 ─────────────────────────────────────────────

func (m *DockerManager) ping(ctx context.Context) error {
	_, err := m.cli.Ping(ctx)
	return err
}

func (m *DockerManager) ensureImage(ctx context.Context, imageRef string) error {
	exists, err := m.ImageExists(ctx, imageRef)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return m.PullImage(ctx, imageRef)
}

// ─── 辅助函数 ─────────────────────────────────────────────

func buildMounts(cfg Config) []mount.Mount {
	var mounts []mount.Mount

	if cfg.Workspace != nil {
		target := cfg.Workspace.Target
		if target == "" {
			target = "/workspace"
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: cfg.Workspace.Source,
			Target: target,
		})
	}

	for _, cm := range cfg.CacheMount {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   cm.Source,
			Target:   cm.Target,
			ReadOnly: cm.ReadOnly,
		})
	}
	for _, mountConfig := range cfg.ReadOnlyMount {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   mountConfig.Source,
			Target:   mountConfig.Target,
			ReadOnly: true,
		})
	}

	return mounts
}

func networkMode(enabled bool) container.NetworkMode {
	if enabled {
		return "default"
	}
	return "none"
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// shellWrap 将用户命令包装在 /bin/sh -c 中执行。
// 所有命令都会被通过 shell 执行，支持 shell 语法（管道、重定向等）。
func shellWrap(commands []string) []string {
	if len(commands) == 0 {
		return nil
	}
	// 如果已经是 /bin/sh -c 格式，直接返回
	if len(commands) == 3 && commands[0] == "/bin/sh" && commands[1] == "-c" {
		return commands
	}
	// 所有命令通过 shell 执行，支持多行 shell 脚本
	return []string{"/bin/sh", "-c", strings.Join(commands, "\n")}
}
