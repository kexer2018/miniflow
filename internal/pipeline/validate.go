package pipeline

import (
	"fmt"
)

// ─── DAG 拓扑校验 ────────────────────────────────────────

// ValidateDAG 对流水线的步骤进行拓扑校验：
//   - 所有步骤名称唯一
//   - depends_on 引用的步骤都存在
//   - 无循环依赖（有向无环图）
//   - 无孤立节点（所有节点都可达）
func ValidateDAG(steps []Step) error {
	if len(steps) == 0 {
		return fmt.Errorf("pipeline must have at least one step")
	}

	// 1. 检查名称唯一性
	names := make(map[string]int) // name → index
	for i, step := range steps {
		if step.Name == "" {
			return fmt.Errorf("step at index %d has empty name", i)
		}
		if _, exists := names[step.Name]; exists {
			return fmt.Errorf("duplicate step name %q", step.Name)
		}
		names[step.Name] = i
	}

	// 2. 检查 depends_on 引用存在
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if _, exists := names[dep]; !exists {
				return fmt.Errorf(
					"step %q depends on %q, which does not exist",
					step.Name, dep,
				)
			}
			// 不能依赖自身
			if dep == step.Name {
				return fmt.Errorf("step %q cannot depend on itself", step.Name)
			}
		}
	}

	// 3. 检测循环依赖（基于拓扑排序）
	if err := detectCycle(steps, names); err != nil {
		return fmt.Errorf("cycle detected: %w", err)
	}

	// 4. 检测孤立节点（没有依赖且没有步骤依赖它——只有入口节点可以无依赖）
	//    只要至少有一个入口节点即可
	hasEntry := false
	for _, step := range steps {
		if len(step.DependsOn) == 0 {
			hasEntry = true
			break
		}
	}
	if !hasEntry {
		return fmt.Errorf("no entry step found: all steps have dependencies, creating a cycle or deadlock")
	}

	return nil
}

// detectCycle 使用 Kahn 算法检测有向图中的环。
func detectCycle(steps []Step, names map[string]int) error {
	n := len(steps)
	inDegree := make([]int, n)
	adj := make([][]int, n) // 邻接表：前置 → 后置

	for _, step := range steps {
		idx := names[step.Name]
		for _, dep := range step.DependsOn {
			depIdx := names[dep]
			adj[depIdx] = append(adj[depIdx], idx)
			inDegree[idx]++
		}
	}

	// 收集入度为 0 的节点（入口）
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	visited := 0
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		visited++

		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if visited != n {
		return fmt.Errorf("graph contains a cycle (%d/%d steps unreachable)", n-visited, n)
	}
	return nil
}

// TopologicalSort 返回步骤的拓扑顺序（广度优先）。
// 并行阶段（Phase 2+）可在此基础扩展。
func TopologicalSort(steps []Step) ([]Step, error) {
	if err := ValidateDAG(steps); err != nil {
		return nil, err
	}

	names := make(map[string]int)
	for i, step := range steps {
		names[step.Name] = i
	}

	n := len(steps)
	inDegree := make([]int, n)
	adj := make([][]int, n)

	for _, step := range steps {
		idx := names[step.Name]
		for _, dep := range step.DependsOn {
			depIdx := names[dep]
			adj[depIdx] = append(adj[depIdx], idx)
			inDegree[idx]++
		}
	}

	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	result := make([]Step, 0, n)
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		result = append(result, steps[u])

		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	return result, nil
}
