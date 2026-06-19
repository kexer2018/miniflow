package pipeline

import (
	"testing"
)

// ─── ValidateDAG Tests ─────────────────────────────────────

func TestValidateDAG_EmptySteps(t *testing.T) {
	err := ValidateDAG([]Step{})
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
	if err.Error() != "pipeline must have at least one step" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidateDAG_SingleStep(t *testing.T) {
	steps := []Step{{Name: "build"}}
	err := ValidateDAG(steps)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateDAG_DuplicateNames(t *testing.T) {
	steps := []Step{
		{Name: "build"},
		{Name: "build"},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
	if err.Error() != `duplicate step name "build"` {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidateDAG_EmptyName(t *testing.T) {
	steps := []Step{
		{Name: ""},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if err.Error() != "step at index 0 has empty name" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidateDAG_InvalidDependsOn(t *testing.T) {
	steps := []Step{
		{Name: "build"},
		{Name: "test", DependsOn: []string{"nonexistent"}},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for invalid depends_on")
	}
	if err.Error() != `step "test" depends on "nonexistent", which does not exist` {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidateDAG_SelfDependency(t *testing.T) {
	steps := []Step{
		{Name: "build", DependsOn: []string{"build"}},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
	if err.Error() != `step "build" cannot depend on itself` {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidateDAG_SimpleCycle(t *testing.T) {
	steps := []Step{
		{Name: "a"},
		{Name: "b", DependsOn: []string{"c"}},
		{Name: "c", DependsOn: []string{"b"}},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
	// a is visited, b and c form a cycle — 2/3 steps unreachable
	expected := "cycle detected: graph contains a cycle (2/3 steps unreachable)"
	if err.Error() != expected {
		t.Errorf("unexpected error:\n  got:  %q\n  want: %q", err.Error(), expected)
	}
}

func TestValidateDAG_FullCycle(t *testing.T) {
	steps := []Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestValidateDAG_NoEntryStep(t *testing.T) {
	// When all steps have dependencies forming a cycle,
	// the cycle detection fires before the "no entry step" check.
	steps := []Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	err := ValidateDAG(steps)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
	// Cycle detection catches it first
	if err.Error() != "cycle detected: graph contains a cycle (2/2 steps unreachable)" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidateDAG_ValidLinearChain(t *testing.T) {
	steps := []Step{
		{Name: "build"},
		{Name: "test", DependsOn: []string{"build"}},
		{Name: "deploy", DependsOn: []string{"test"}},
	}
	err := ValidateDAG(steps)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateDAG_DisconnectedSubgraph(t *testing.T) {
	steps := []Step{
		{Name: "lint"},
		{Name: "build"},
		{Name: "test", DependsOn: []string{"build"}},
	}
	err := ValidateDAG(steps)
	if err != nil {
		t.Errorf("expected no error for disconnected subgraph, got: %v", err)
	}
}

func TestValidateDAG_FanOutFanIn(t *testing.T) {
	steps := []Step{
		{Name: "build"},
		{Name: "unit-test", DependsOn: []string{"build"}},
		{Name: "integration-test", DependsOn: []string{"build"}},
		{Name: "package", DependsOn: []string{"unit-test", "integration-test"}},
	}
	err := ValidateDAG(steps)
	if err != nil {
		t.Errorf("expected no error for fan-out/fan-in, got: %v", err)
	}
}

func TestValidateDAG_MultipleEntryPoints(t *testing.T) {
	steps := []Step{
		{Name: "fetch-deps"},
		{Name: "generate-code"},
		{Name: "build", DependsOn: []string{"fetch-deps", "generate-code"}},
	}
	err := ValidateDAG(steps)
	if err != nil {
		t.Errorf("expected no error for multiple entry points, got: %v", err)
	}
}

// ─── TopologicalSort Tests ─────────────────────────────────

func TestTopologicalSort_SingleNode(t *testing.T) {
	steps := []Step{{Name: "build"}}

	sorted, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 1 {
		t.Fatalf("expected 1 step, got %d", len(sorted))
	}
	if sorted[0].Name != "build" {
		t.Errorf("expected 'build', got %q", sorted[0].Name)
	}
}

func TestTopologicalSort_LinearChain(t *testing.T) {
	steps := []Step{
		{Name: "build"},
		{Name: "test", DependsOn: []string{"build"}},
		{Name: "deploy", DependsOn: []string{"test"}},
	}

	sorted, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(sorted))
	}

	// Linear chain must maintain order: build → test → deploy
	expected := []string{"build", "test", "deploy"}
	for i, step := range sorted {
		if step.Name != expected[i] {
			t.Errorf("position %d: expected %q, got %q", i, expected[i], step.Name)
		}
	}
}

func TestTopologicalSort_ReverseOrderInput(t *testing.T) {
	steps := []Step{
		{Name: "deploy", DependsOn: []string{"test"}},
		{Name: "build"},
		{Name: "test", DependsOn: []string{"build"}},
	}

	sorted, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(sorted))
	}

	// Must be: build → test → deploy
	if sorted[0].Name != "build" {
		t.Errorf("first step should be 'build', got %q", sorted[0].Name)
	}
	if sorted[1].Name != "test" {
		t.Errorf("second step should be 'test', got %q", sorted[1].Name)
	}
	if sorted[2].Name != "deploy" {
		t.Errorf("third step should be 'deploy', got %q", sorted[2].Name)
	}
}

func TestTopologicalSort_FanOut(t *testing.T) {
	steps := []Step{
		{Name: "build"},
		{Name: "lint", DependsOn: []string{"build"}},
		{Name: "test", DependsOn: []string{"build"}},
	}

	sorted, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(sorted))
	}

	// build must be first
	if sorted[0].Name != "build" {
		t.Errorf("first step should be 'build', got %q", sorted[0].Name)
	}
}

func TestTopologicalSort_FanIn(t *testing.T) {
	steps := []Step{
		{Name: "compile"},
		{Name: "test"},
		{Name: "package", DependsOn: []string{"compile", "test"}},
	}

	sorted, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(sorted))
	}

	// package must be last
	if sorted[2].Name != "package" {
		t.Errorf("last step should be 'package', got %q", sorted[2].Name)
	}
}

func TestTopologicalSort_EmptySteps(t *testing.T) {
	_, err := TopologicalSort([]Step{})
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestTopologicalSort_Cycle(t *testing.T) {
	steps := []Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	_, err := TopologicalSort(steps)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestTopologicalSort_DiamondDependency(t *testing.T) {
	steps := []Step{
		{Name: "setup"},
		{Name: "compile", DependsOn: []string{"setup"}},
		{Name: "generate", DependsOn: []string{"setup"}},
		{Name: "build", DependsOn: []string{"compile", "generate"}},
		{Name: "deploy", DependsOn: []string{"build"}},
	}

	sorted, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(sorted))
	}

	// Verify dependency order is preserved:
	// setup before compile, setup before generate, compile/generate before build, build before deploy
	positions := make(map[string]int)
	for i, s := range sorted {
		positions[s.Name] = i
	}

	checks := []struct {
		name     string
		before   string
		after    string
	}{
		{"setup -> compile", "setup", "compile"},
		{"setup -> generate", "setup", "generate"},
		{"compile -> build", "compile", "build"},
		{"generate -> build", "generate", "build"},
		{"build -> deploy", "build", "deploy"},
	}
	for _, c := range checks {
		if positions[c.before] > positions[c.after] {
			t.Errorf("%s: %q (pos %d) should be before %q (pos %d)",
				c.name, c.before, positions[c.before], c.after, positions[c.after])
		}
	}
}
