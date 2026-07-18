package changegraph

import (
	"container/heap"
	"sort"
)

const MaxCohortDependencies = 3

func StableOrder(current []int, edges []Edge) []int {
	result := append([]int(nil), current...)
	if len(result) < 2 || len(edges) == 0 {
		return result
	}
	position := make(map[int]int, len(result))
	for index, file := range result {
		position[file] = index
	}
	internal := make([]Edge, 0, len(edges))
	seenEdges := map[Edge]bool{}
	for _, edge := range edges {
		if _, ok := position[edge.Importer]; !ok {
			continue
		}
		if _, ok := position[edge.Imported]; !ok || edge.Importer == edge.Imported {
			continue
		}
		if seenEdges[edge] {
			continue
		}
		seenEdges[edge] = true
		internal = append(internal, edge)
	}
	if len(internal) == 0 {
		return result
	}
	component, componentSize := stronglyConnected(result, internal, position)
	dependencies := make(map[int][]int, len(result))
	dependents := make(map[int][]int, len(result))
	for _, edge := range internal {
		if component[edge.Importer] == component[edge.Imported] && componentSize[component[edge.Importer]] > 1 {
			continue
		}
		dependencies[edge.Importer] = append(dependencies[edge.Importer], edge.Imported)
		dependents[edge.Imported] = append(dependents[edge.Imported], edge.Importer)
	}
	cycleMembers := map[int][]int{}
	for _, file := range result {
		componentIndex := component[file]
		if componentSize[componentIndex] > 1 {
			cycleMembers[componentIndex] = append(cycleMembers[componentIndex], file)
		}
	}
	for _, members := range cycleMembers {
		for index := 1; index < len(members); index++ {
			later, earlier := members[index], members[index-1]
			dependencies[later] = append(dependencies[later], earlier)
			dependents[earlier] = append(dependents[earlier], later)
		}
	}

	ready := &fileHeap{position: position}
	heap.Init(ready)
	for _, file := range result {
		if len(dependencies[file]) == 0 {
			heap.Push(ready, file)
		}
	}
	ordered := make([]int, 0, len(result))
	for ready.Len() > 0 {
		file := heap.Pop(ready).(int)
		ordered = append(ordered, file)
		for _, dependent := range dependents[file] {
			dependencies[dependent] = removeDependency(dependencies[dependent], file)
			if len(dependencies[dependent]) == 0 {
				heap.Push(ready, dependent)
			}
		}
	}
	if len(ordered) != len(result) {
		return result
	}
	return ordered
}

func CohortDependencies(cohorts [][]int, edges []Edge) [][]int {
	result := make([][]int, len(cohorts))
	fileCohort := map[int]int{}
	for cohortIndex, files := range cohorts {
		for _, file := range files {
			fileCohort[file] = cohortIndex
		}
	}
	weights := make([]map[int]int, len(cohorts))
	seenEdges := map[Edge]bool{}
	for _, edge := range edges {
		if seenEdges[edge] {
			continue
		}
		seenEdges[edge] = true
		dependent, dependentOK := fileCohort[edge.Importer]
		dependency, dependencyOK := fileCohort[edge.Imported]
		if !dependentOK || !dependencyOK || dependency >= dependent {
			continue
		}
		if weights[dependent] == nil {
			weights[dependent] = map[int]int{}
		}
		weights[dependent][dependency]++
	}
	for dependent, inbound := range weights {
		result[dependent] = rankedDependencies(inbound)
	}
	for index := range result {
		if result[index] == nil {
			result[index] = []int{}
		}
	}
	return result
}

func StableCohortOrder(cohorts [][]int, edges []Edge) ([]int, [][]int) {
	current := make([]int, len(cohorts))
	position := make(map[int]int, len(cohorts))
	for index := range cohorts {
		current[index] = index
		position[index] = index
	}
	cohortEdges, weights := aggregateCohortEdges(cohorts, edges)
	order := StableOrder(current, cohortEdges)
	orderedPosition := make(map[int]int, len(order))
	for index, original := range order {
		orderedPosition[original] = index
	}
	component, componentSize := stronglyConnected(current, cohortEdges, position)
	dependencies := make([][]int, len(cohorts))
	orderedWeights := make([]map[int]int, len(cohorts))
	for dependent, inbound := range weights {
		for dependency, weight := range inbound {
			if component[dependent] == component[dependency] && componentSize[component[dependent]] > 1 {
				continue
			}
			orderedDependent := orderedPosition[dependent]
			orderedDependency := orderedPosition[dependency]
			if orderedDependency >= orderedDependent {
				panic("changegraph: cohort dependency violates topological order")
			}
			if orderedWeights[orderedDependent] == nil {
				orderedWeights[orderedDependent] = map[int]int{}
			}
			orderedWeights[orderedDependent][orderedDependency] = weight
		}
	}
	for dependent, inbound := range orderedWeights {
		dependencies[dependent] = rankedDependencies(inbound)
	}
	for index := range dependencies {
		if dependencies[index] == nil {
			dependencies[index] = []int{}
		}
	}
	return order, dependencies
}

func aggregateCohortEdges(cohorts [][]int, edges []Edge) ([]Edge, []map[int]int) {
	fileCohort := map[int]int{}
	for cohortIndex, files := range cohorts {
		for _, file := range files {
			fileCohort[file] = cohortIndex
		}
	}
	weights := make([]map[int]int, len(cohorts))
	seenEdges := map[Edge]bool{}
	for _, edge := range edges {
		if seenEdges[edge] {
			continue
		}
		seenEdges[edge] = true
		dependent, dependentOK := fileCohort[edge.Importer]
		dependency, dependencyOK := fileCohort[edge.Imported]
		if !dependentOK || !dependencyOK || dependent == dependency {
			continue
		}
		if weights[dependent] == nil {
			weights[dependent] = map[int]int{}
		}
		weights[dependent][dependency]++
	}
	var result []Edge
	for dependent, inbound := range weights {
		for dependency := range inbound {
			result = append(result, Edge{Importer: dependent, Imported: dependency})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Importer != result[j].Importer {
			return result[i].Importer < result[j].Importer
		}
		return result[i].Imported < result[j].Imported
	})
	return result, weights
}

func rankedDependencies(inbound map[int]int) []int {
	dependencies := make([]int, 0, len(inbound))
	for dependency := range inbound {
		dependencies = append(dependencies, dependency)
	}
	sort.Slice(dependencies, func(i, j int) bool {
		left, right := dependencies[i], dependencies[j]
		if inbound[left] != inbound[right] {
			return inbound[left] > inbound[right]
		}
		return left < right
	})
	if len(dependencies) > MaxCohortDependencies {
		dependencies = dependencies[:MaxCohortDependencies]
	}
	return dependencies
}

func stronglyConnected(nodes []int, edges []Edge, position map[int]int) (map[int]int, map[int]int) {
	adjacency := make(map[int][]int, len(nodes))
	for _, edge := range edges {
		adjacency[edge.Importer] = append(adjacency[edge.Importer], edge.Imported)
	}
	for node := range adjacency {
		sort.Slice(adjacency[node], func(i, j int) bool {
			return position[adjacency[node][i]] < position[adjacency[node][j]]
		})
	}
	index := 0
	componentIndex := 0
	indexes := map[int]int{}
	lowLink := map[int]int{}
	onStack := map[int]bool{}
	stack := make([]int, 0, len(nodes))
	components := map[int]int{}
	componentSize := map[int]int{}
	var visit func(int)
	visit = func(node int) {
		indexes[node] = index
		lowLink[node] = index
		index++
		stack = append(stack, node)
		onStack[node] = true
		for _, next := range adjacency[node] {
			if _, seen := indexes[next]; !seen {
				visit(next)
				lowLink[node] = min(lowLink[node], lowLink[next])
			} else if onStack[next] {
				lowLink[node] = min(lowLink[node], indexes[next])
			}
		}
		if lowLink[node] != indexes[node] {
			return
		}
		for {
			last := len(stack) - 1
			member := stack[last]
			stack = stack[:last]
			onStack[member] = false
			components[member] = componentIndex
			componentSize[componentIndex]++
			if member == node {
				break
			}
		}
		componentIndex++
	}
	for _, node := range nodes {
		if _, seen := indexes[node]; !seen {
			visit(node)
		}
	}
	return components, componentSize
}

func removeDependency(dependencies []int, completed int) []int {
	for index, dependency := range dependencies {
		if dependency == completed {
			return append(dependencies[:index], dependencies[index+1:]...)
		}
	}
	return dependencies
}

type fileHeap struct {
	files    []int
	position map[int]int
}

func (h fileHeap) Len() int { return len(h.files) }
func (h fileHeap) Less(i, j int) bool {
	return h.position[h.files[i]] < h.position[h.files[j]]
}
func (h fileHeap) Swap(i, j int)   { h.files[i], h.files[j] = h.files[j], h.files[i] }
func (h *fileHeap) Push(value any) { h.files = append(h.files, value.(int)) }
func (h *fileHeap) Pop() any {
	last := len(h.files) - 1
	value := h.files[last]
	h.files = h.files[:last]
	return value
}
