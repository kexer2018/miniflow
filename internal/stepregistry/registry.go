// Package stepregistry defines built-in and trusted external typed Steps.
package stepregistry

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	internalpipeline "github.com/kexer2018/miniflow/internal/pipeline"
	pipelinespec "github.com/kexer2018/miniflow/pkg/pipeline"
)

const (
	TypeScriptRun       = "script.run"
	TypeGitCheckout     = "git.checkout"
	TypeFileOperation   = "file.operation"
	TypeCacheRestore    = "cache.restore"
	TypeCacheSave       = "cache.save"
	TypeArtifactSave    = "artifact.save"
	TypeArtifactRestore = "artifact.restore"
	extensionMountBase  = "/opt/miniflow/extensions"
)

var extensionIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)+$`)
var semanticVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)

// Definition describes a Step type for API and UI consumers.
type Definition struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Description string         `json:"description"`
	Version     string         `json:"version,omitempty"`
	Source      string         `json:"source"`
	Defaults    map[string]any `json:"defaults,omitempty"`
	Schema      map[string]any `json:"schema"`
}

// Manifest is the trusted on-disk ScriptStepExtension v1 format.
type Manifest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		ID          string `json:"id"`
		Version     string `json:"version"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"metadata"`
	Spec struct {
		Runtime struct {
			Image  string `json:"image"`
			Script string `json:"script"`
		} `json:"runtime"`
		Inputs  map[string]any `json:"inputs"`
		Outputs []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"outputs,omitempty"`
	} `json:"spec"`
}

type externalStep struct {
	manifest Manifest
	bundle   string
}

// Registry is immutable after construction and can safely be shared by API,
// validation, and asynchronous run execution.
type Registry struct {
	definitions map[string]Definition
	external    map[string]externalStep
}

func New(directories ...string) (*Registry, error) {
	r := &Registry{definitions: make(map[string]Definition), external: make(map[string]externalStep)}
	for _, definition := range builtinDefinitions() {
		r.definitions[definition.ID] = definition
	}
	for _, directory := range directories {
		if err := r.loadDirectory(directory); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func Default() *Registry {
	r, err := New()
	if err != nil {
		panic(err)
	}
	return r
}

// Builtins remains compatible with the existing public API.
func Builtins() []Definition { return Default().List() }

func (r *Registry) List() []Definition {
	definitions := make([]Definition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func (r *Registry) ValidatePipeline(spec *pipelinespec.PipelineSpec) error {
	for _, step := range spec.Steps {
		if _, err := r.Compile(step); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) Compile(spec pipelinespec.StepSpec) (internalpipeline.Step, error) {
	step := internalpipeline.Step{Name: spec.Name, Image: spec.Image, Commands: spec.Commands, DependsOn: spec.DependsOn, Env: spec.Env, Entrypoint: spec.Entrypoint, SSHAgent: spec.SSHAgent}
	if external, ok := r.external[spec.Type]; ok {
		return r.compileExternal(step, spec, external)
	}
	switch spec.Type {
	case "":
		return step, nil
	case TypeScriptRun:
		script, ok := spec.With["script"].(string)
		if !ok || script == "" {
			return internalpipeline.Step{}, fmt.Errorf("step %q requires with.script", spec.Name)
		}
		step.Commands = []string{script}
		return step, nil
	case TypeGitCheckout, TypeCacheRestore, TypeCacheSave, TypeArtifactSave, TypeArtifactRestore:
		step.Operation = &internalpipeline.Operation{Type: spec.Type, With: spec.With}
		return step, nil
	case TypeFileOperation:
		command, err := compileFileOperation(spec.With)
		if err != nil {
			return internalpipeline.Step{}, fmt.Errorf("step %q: %w", spec.Name, err)
		}
		if step.Image == "" {
			step.Image = "alpine:latest"
		}
		step.Commands = []string{command}
		return step, nil
	default:
		return internalpipeline.Step{}, fmt.Errorf("step %q has unsupported type %q", spec.Name, spec.Type)
	}
}

// Compile keeps existing callers compatible with the built-in registry.
func Compile(spec pipelinespec.StepSpec) (internalpipeline.Step, error) {
	return Default().Compile(spec)
}

func (r *Registry) compileExternal(step internalpipeline.Step, spec pipelinespec.StepSpec, external externalStep) (internalpipeline.Step, error) {
	if err := validateInputs(external.manifest.Spec.Inputs, spec.With); err != nil {
		return internalpipeline.Step{}, fmt.Errorf("step %q: %w", spec.Name, err)
	}
	for _, entry := range spec.Env {
		if strings.HasPrefix(entry, "MINIFLOW_INPUT_") {
			return internalpipeline.Step{}, fmt.Errorf("step %q: env prefix MINIFLOW_INPUT_ is reserved for extension inputs", spec.Name)
		}
	}
	inputEnv, err := inputEnvironment(spec.With)
	if err != nil {
		return internalpipeline.Step{}, fmt.Errorf("step %q: %w", spec.Name, err)
	}
	step.Image = external.manifest.Spec.Runtime.Image
	step.Commands = []string{"exec sh " + shellQuote(filepath.ToSlash(filepath.Join(extensionMountBase, external.manifest.Metadata.ID, external.manifest.Spec.Runtime.Script)))}
	step.Env = inputEnv
	step.Extension = &internalpipeline.Extension{Source: external.bundle, Target: filepath.ToSlash(filepath.Join(extensionMountBase, external.manifest.Metadata.ID))}
	return step, nil
}

func (r *Registry) loadDirectory(directory string) error {
	if directory == "" {
		return nil
	}
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("step directory %q: %w", directory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("step directory %q is not a directory", directory)
	}
	manifestPaths := []string{}
	rootManifest := filepath.Join(directory, "step.json")
	if _, err := os.Stat(rootManifest); err == nil {
		manifestPaths = append(manifestPaths, rootManifest)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read step directory %q: %w", directory, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(directory, entry.Name(), "step.json")
			if _, err := os.Stat(path); err == nil {
				manifestPaths = append(manifestPaths, path)
			}
		}
	}
	for _, path := range manifestPaths {
		if err := r.loadManifest(path); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) loadManifest(path string) error {
	if err := validateTrustedFile(path); err != nil {
		return fmt.Errorf("extension manifest %q: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse extension manifest %q: %w", path, err)
	}
	if err := validateManifest(manifest, filepath.Dir(path)); err != nil {
		return fmt.Errorf("extension manifest %q: %w", path, err)
	}
	if _, exists := r.definitions[manifest.Metadata.ID]; exists {
		return fmt.Errorf("extension %q duplicates an existing Step type", manifest.Metadata.ID)
	}
	r.external[manifest.Metadata.ID] = externalStep{manifest: manifest, bundle: filepath.Dir(path)}
	if !strings.Contains(manifest.Spec.Runtime.Image, "@sha256:") {
		slog.Warn("extension image is not pinned by digest", "extension", manifest.Metadata.ID, "image", manifest.Spec.Runtime.Image)
	}
	r.definitions[manifest.Metadata.ID] = Definition{ID: manifest.Metadata.ID, Name: manifest.Metadata.Name, Group: "Extension", Description: manifest.Metadata.Description, Version: manifest.Metadata.Version, Source: "external", Schema: manifest.Spec.Inputs}
	return nil
}

func builtinDefinitions() []Definition {
	return []Definition{
		{ID: TypeScriptRun, Name: "Shell Script", Group: "Script", Description: "Run a user-provided script in an ephemeral Docker container.", Source: "builtin", Schema: map[string]any{"type": "object", "required": []string{"script"}, "properties": map[string]any{"script": map[string]any{"type": "string"}}}},
		operationDefinition(TypeGitCheckout, "Git Checkout", "Source", "Clone a Git repository into the shared workspace.", []string{"repository"}), operationDefinition(TypeFileOperation, "File Operation", "Workspace", "Copy, move, delete, create, archive, or extract workspace files.", []string{"operation"}), operationDefinition(TypeCacheRestore, "Cache Restore", "Storage", "Restore a local cache into the workspace.", []string{"key", "path"}), operationDefinition(TypeCacheSave, "Cache Save", "Storage", "Save a workspace path into the local cache.", []string{"key", "path"}), operationDefinition(TypeArtifactSave, "Artifact Save", "Storage", "Archive workspace files for this run.", []string{"name", "path"}), operationDefinition(TypeArtifactRestore, "Artifact Restore", "Storage", "Restore an artifact archive into the workspace.", []string{"name"}),
	}
}

func operationDefinition(id, name, group, description string, required []string) Definition {
	return Definition{ID: id, Name: name, Group: group, Description: description, Source: "builtin", Schema: map[string]any{"type": "object", "required": required, "properties": map[string]any{}}}
}

func validateManifest(manifest Manifest, bundle string) error {
	if manifest.APIVersion != "miniflow.dev/v1" || manifest.Kind != "ScriptStepExtension" {
		return fmt.Errorf("apiVersion must be miniflow.dev/v1 and kind must be ScriptStepExtension")
	}
	if !extensionIDPattern.MatchString(manifest.Metadata.ID) || !semanticVersionPattern.MatchString(manifest.Metadata.Version) || manifest.Metadata.Name == "" {
		return fmt.Errorf("metadata requires a reverse-domain id, semantic version, and name")
	}
	if manifest.Spec.Runtime.Image == "" {
		return fmt.Errorf("spec.runtime.image is required")
	}
	if !safeRelativePath(manifest.Spec.Runtime.Script) {
		return fmt.Errorf("spec.runtime.script must be a relative bundle path")
	}
	if err := validateTrustedDirectory(bundle); err != nil {
		return fmt.Errorf("bundle: %w", err)
	}
	if err := validateTrustedFile(filepath.Join(bundle, manifest.Spec.Runtime.Script)); err != nil {
		return fmt.Errorf("runtime script: %w", err)
	}
	if manifest.Spec.Inputs == nil || manifest.Spec.Inputs["type"] != "object" {
		return fmt.Errorf("spec.inputs must be a JSON Schema object")
	}
	for _, output := range manifest.Spec.Outputs {
		if output.Name == "" || !safeRelativePath(output.Path) {
			return fmt.Errorf("spec.outputs entries require a name and workspace-relative path")
		}
	}
	return nil
}

func validateTrustedDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a real directory, not a symbolic link")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("must not be writable by group or other")
	}
	return nil
}

func validateTrustedFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular file, not a symbolic link")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("must not be writable by group or other")
	}
	return nil
}

func validateInputs(schema, values map[string]any) error {
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	if required == nil {
		if names, ok := schema["required"].([]string); ok {
			for _, name := range names {
				required = append(required, name)
			}
		}
	}
	for _, raw := range required {
		name, _ := raw.(string)
		if _, ok := values[name]; !ok {
			return fmt.Errorf("missing required input %q", name)
		}
	}
	if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		for name := range values {
			if _, ok := properties[name]; !ok {
				return fmt.Errorf("unknown input %q", name)
			}
		}
	}
	for name, value := range values {
		property, exists := properties[name]
		if !exists {
			continue
		}
		definition, _ := property.(map[string]any)
		if kind, _ := definition["type"].(string); kind != "" && !matchesJSONType(value, kind) {
			return fmt.Errorf("input %q must be %s", name, kind)
		}
		if enum, ok := definition["enum"].([]any); ok {
			found := false
			for _, allowed := range enum {
				if fmt.Sprint(allowed) == fmt.Sprint(value) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("input %q is not an allowed value", name)
			}
		}
	}
	return nil
}

func matchesJSONType(value any, kind string) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number", "integer":
		_, ok := value.(float64)
		return ok
	default:
		return true
	}
}
func inputEnvironment(values map[string]any) ([]string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(key) {
			return nil, fmt.Errorf("input name %q is invalid", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		encoded, err := json.Marshal(values[key])
		if err != nil {
			return nil, err
		}
		value := string(encoded)
		if text, ok := values[key].(string); ok {
			value = text
		}
		env = append(env, "MINIFLOW_INPUT_"+strings.ToUpper(key)+"="+value)
	}
	return env, nil
}
func safeRelativePath(value string) bool {
	return value != "" && !filepath.IsAbs(value) && filepath.Clean(value) != ".." && !strings.HasPrefix(filepath.Clean(value), ".."+string(filepath.Separator))
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func compileFileOperation(values map[string]any) (string, error) {
	operation, _ := values["operation"].(string)
	source, _ := values["source"].(string)
	target, _ := values["target"].(string)
	switch operation {
	case "mkdir":
		if target == "" {
			return "", fmt.Errorf("mkdir requires with.target")
		}
		return "mkdir -p " + shellQuote(target), nil
	case "copy":
		if source == "" || target == "" {
			return "", fmt.Errorf("copy requires with.source and with.target")
		}
		return "cp -a " + shellQuote(source) + " " + shellQuote(target), nil
	case "move":
		if source == "" || target == "" {
			return "", fmt.Errorf("move requires with.source and with.target")
		}
		return "mv " + shellQuote(source) + " " + shellQuote(target), nil
	case "delete":
		if source == "" {
			return "", fmt.Errorf("delete requires with.source")
		}
		return "rm -rf " + shellQuote(source), nil
	case "archive":
		if source == "" || target == "" {
			return "", fmt.Errorf("archive requires with.source and with.target")
		}
		return "tar -czf " + shellQuote(target) + " " + shellQuote(source), nil
	case "extract":
		if source == "" || target == "" {
			return "", fmt.Errorf("extract requires with.source and with.target")
		}
		return "mkdir -p " + shellQuote(target) + " && tar -xzf " + shellQuote(source) + " -C " + shellQuote(target), nil
	default:
		return "", fmt.Errorf("unsupported file operation %q", operation)
	}
}
