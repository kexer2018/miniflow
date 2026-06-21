package secret

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// ─── 常量 ──────────────────────────────────────────────

type CredentialType string

const (
	CredTypeToken        CredentialType = "token"
	CredTypeSSHKey       CredentialType = "ssh_key"
	CredTypeUsernamePass CredentialType = "username_password"
	CredTypeEnv          CredentialType = "env"
)

// ─── 类型 ──────────────────────────────────────────────

// Credential 表示一个凭证条目。
type Credential struct {
	ID       string         `json:"id"`
	Match    string         `json:"match"`
	Type     CredentialType `json:"type"`
	Value    string         `json:"value,omitempty"`
	Username string         `json:"username,omitempty"`
	Password string         `json:"password,omitempty"`
}

// credentialsFile 是磁盘上的 JSON 结构。
type credentialsFile struct {
	Version     string        `json:"version"`
	Credentials []*Credential `json:"credentials"`
}

// CredentialStore 管理凭证的加载、匹配和解析。
type CredentialStore struct {
	creds []*Credential
}

// NewCredentialStore 创建空的凭证存储。
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{creds: make([]*Credential, 0)}
}

// LoadFromFile 从 JSON 文件加载凭证。
// path 为空时返回空 store（无错误）。
// 文件不存在时返回空 store（非致命）。
func LoadFromFile(path string) (*CredentialStore, error) {
	if path == "" {
		return NewCredentialStore(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("credentials file not found, using empty store", "path", path)
			return NewCredentialStore(), nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	// 权限检查（仅 Unix）
	if fi, statErr := os.Stat(path); statErr == nil {
		perm := fi.Mode().Perm()
		if perm != 0o600 && perm != 0o400 {
			slog.Warn("credentials file permissions should be 0600",
				"path", path, "current", fmt.Sprintf("%o", perm))
		}
	}

	var file credentialsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	slog.Debug("credentials loaded", "path", path, "count", len(file.Credentials))
	return &CredentialStore{creds: file.Credentials}, nil
}

// MustLoad 加载凭证，失败时返回空 store（非致命，用于 main 中兜底）。
func MustLoad(path string) *CredentialStore {
	store, err := LoadFromFile(path)
	if err != nil {
		slog.Error("failed to load credentials", "error", err)
		return NewCredentialStore()
	}
	return store
}

// Match 根据 repository URL 匹配最合适的凭证。
// 匹配策略：最长前缀匹配 → 先注册优先。
// 返回 nil 表示无匹配。
func (s *CredentialStore) Match(repoURL string) *Credential {
	if s == nil {
		return nil
	}

	var best *Credential
	bestLen := 0

	for _, c := range s.creds {
		if c.Match == "*" {
			// 兜底：记录但不立即返回（有更长匹配优先）
			if bestLen == 0 {
				best = c
				bestLen = 1
			}
			continue
		}
		if strings.HasPrefix(repoURL, c.Match) && len(c.Match) > bestLen {
			best = c
			bestLen = len(c.Match)
		}
	}

	if best != nil {
		slog.Debug("credential matched", "id", best.ID, "match", best.Match, "repo", repoURL)
	}
	return best
}

// ResolveSecret 根据 secret ID 返回对应的值。
// 对 type=env 返回 "KEY=VALUE" 格式字符串。
// 对其他 type 返回 Value 字段。
// ok=false 表示未找到。
func (s *CredentialStore) ResolveSecret(id string) (string, bool) {
	if s == nil {
		return "", false
	}
	for _, c := range s.creds {
		if c.ID == id {
			return c.Value, true
		}
	}
	return "", false
}

// ResolveSecretEnv 返回适合直接注入容器 env 的 "KEY=VALUE" 格式值。
// CredTypeEnv 类型的凭证值本身已是 "KEY=VALUE" 格式，直接返回。
// 其他类型以 `id=value` 格式构造。
// ok=false 表示未找到。
func (s *CredentialStore) ResolveSecretEnv(id string) (string, bool) {
	if s == nil {
		return "", false
	}
	for _, c := range s.creds {
		if c.ID == id {
			if c.Type == CredTypeEnv {
				return c.Value, true
			}
			return id + "=" + c.Value, true
		}
	}
	return "", false
}

// AllSecretsMap 返回所有 type=env 凭证的 key→value 映射。
// 用于批量注册到 log.Sanitizer。
func (s *CredentialStore) AllSecretsMap() map[string]string {
	result := make(map[string]string)
	if s == nil {
		return result
	}
	for _, c := range s.creds {
		if c.Type == CredTypeEnv {
			// "CODECOV_TOKEN=xxxx" → {"CODECOV_TOKEN": "xxxx"}
			if k, v, ok := strings.Cut(c.Value, "="); ok {
				result[k] = v
			} else {
				result[c.ID] = c.Value
			}
		} else {
			result[c.ID] = c.Value
		}
	}
	return result
}
