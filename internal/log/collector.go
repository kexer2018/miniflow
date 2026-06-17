// Package log 提供日志实时采集、脱敏过滤和分类功能。
package log

import (
	"bufio"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// ─── 日志采集器 ───────────────────────────────────────────

// Collector 实时采集 stdout/stderr 日志流。
//
// 支持按行回调，被 collector 收集的日志会被传递给下游的
// Sanitizer 和 Classifier 处理。
type Collector struct {
	mu       sync.Mutex
	buf      strings.Builder
	lines    []string
	callback LogLineCallback
}

// LogLineCallback 是每行日志的回调函数。
type LogLineCallback func(line string)

// NewCollector 创建日志采集器。
func NewCollector() *Collector {
	return &Collector{
		lines: make([]string, 0, 1024),
	}
}

// SetCallback 设置逐行回调（用于实时流式处理）。
func (c *Collector) SetCallback(cb LogLineCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callback = cb
}

// Collect 从 reader 中读取所有日志内容并逐行记录。
// 返回完整日志文本。
func (c *Collector) Collect(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*64), 1024*1024) // 64K buffer, 1MB max line

	for scanner.Scan() {
		line := scanner.Text()
		c.addLine(line)
	}

	if err := scanner.Err(); err != nil {
		return c.String(), err
	}

	return c.String(), nil
}

// CollectLines 从 reader 中读取日志，返回逐行切片。
func (c *Collector) CollectLines(reader io.Reader) ([]string, error) {
	_, err := c.Collect(reader)
	if err != nil {
		return nil, err
	}
	return c.Lines(), nil
}

// addLine 添加一行日志。
func (c *Collector) addLine(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.buf.WriteString(line)
	c.buf.WriteString("\n")
	c.lines = append(c.lines, line)

	if c.callback != nil {
		// 在 goroutine 中执行回调，避免阻塞采集
		go func(l string) {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("log callback panicked", "recover", r)
				}
			}()
			c.callback(l)
		}(line)
	}
}

// Lines 返回所有已采集的行。
func (c *Collector) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, len(c.lines))
	copy(result, c.lines)
	return result
}

// String 返回所有已采集的日志文本。
func (c *Collector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSuffix(c.buf.String(), "\n")
}

// Reset 清空采集器内部状态。
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Reset()
	c.lines = make([]string, 0, 1024)
}

// ─── 合并读取器 ───────────────────────────────────────────

// MultiReader 将多个 io.Reader 合并为一个，逐行交错输出。
type MultiReader struct {
	readers []io.Reader
}

// NewMultiReader 创建合并读取器。
func NewMultiReader(readers ...io.Reader) *MultiReader {
	return &MultiReader{readers: readers}
}

// Read 实现 io.Reader。
// 注意：这是一个简化实现，逐读取器交替读取。
func (mr *MultiReader) Read(p []byte) (n int, err error) {
	for _, r := range mr.readers {
		if r == nil {
			continue
		}
		nn, err := r.Read(p[n:])
		n += nn
		if err != nil {
			if err == io.EOF {
				continue
			}
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}
	return 0, io.EOF
}
