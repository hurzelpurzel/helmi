package overlay

import (
	"sort"

	"helmi/internal/errs"
	"helmi/internal/resources"
	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

// OverlayAnalyzer analyzes differences between overlays.
type OverlayAnalyzer struct {
	Differences        map[string]map[string]any
	Parameterizable    map[string]struct{}
	ResourcesByOverlay map[string][]*yaml.Node
}

// AnalyzeDifferences analyzes overlay differences.
func (a *OverlayAnalyzer) AnalyzeDifferences(overlayResources map[string][]*yaml.Node) error {
	if _, ok := overlayResources["base"]; !ok {
		return errs.Configurationf("Overlay analysis requires a 'base' resource set")
	}
	a.ResourcesByOverlay = overlayResources
	a.Differences = make(map[string]map[string]any)
	a.Parameterizable = make(map[string]struct{})
	baseIdx, err := resources.IndexResources(overlayResources["base"])
	if err != nil {
		return err
	}
	ovNames := make([]string, 0, len(overlayResources))
	for n := range overlayResources {
		if n != "base" {
			ovNames = append(ovNames, n)
		}
	}
	sort.Strings(ovNames)
	for _, name := range ovNames {
		ovIdx, err := resources.IndexResources(overlayResources[name])
		if err != nil {
			return err
		}
		changes := make(map[string]any)
		all := make(map[resources.Identity]struct{})
		for id := range baseIdx.Items {
			all[id] = struct{}{}
		}
		for id := range ovIdx.Items {
			all[id] = struct{}{}
		}
		idents := make([]resources.Identity, 0, len(all))
		for id := range all {
			idents = append(idents, id)
		}
		sort.Slice(idents, func(i, j int) bool { return idents[i].Less(idents[j]) })
		for _, id := range idents {
			key := id.Text()
			_, inBase := baseIdx.Items[id]
			_, inOv := ovIdx.Items[id]
			if !inBase {
				changes[key] = map[string]any{"change": "added", "resource": cloneNode(ovIdx.Items[id])}
				continue
			}
			if !inOv {
				changes[key] = map[string]any{"change": "removed"}
				continue
			}
			eq, err := yamlutil.DeepEqual(baseIdx.Items[id], ovIdx.Items[id])
			if err != nil {
				return err
			}
			if !eq {
				fields := make(map[string]any)
				a.deepCompare(baseIdx.Items[id], ovIdx.Items[id], "", fields)
				changes[key] = map[string]any{"change": "modified", "fields": fields}
				for p := range fields {
					a.Parameterizable[p] = struct{}{}
				}
			}
		}
		a.Differences[name] = changes
	}
	return nil
}

func cloneNode(n *yaml.Node) *yaml.Node {
	return yamlutil.Clone(n)
}

func (a *OverlayAnalyzer) deepCompare(base, ov *yaml.Node, path string, result map[string]any) {
	if base == nil || ov == nil {
		if !equalNodes(base, ov) {
			result[pathOrDollar(path, base, ov)] = decodeNode(ov)
		}
		return
	}
	if base.Kind != ov.Kind || base.Tag != ov.Tag {
		result[pathOrDollar(path, base, ov)] = decodeNode(ov)
		return
	}
	switch base.Kind {
	case yaml.MappingNode:
		keys := make(map[string]bool)
		for i := 0; i+1 < len(base.Content); i += 2 {
			k := base.Content[i]
			if ks, ok := scalar(k); ok {
				keys[ks] = true
			}
		}
		for i := 0; i+1 < len(ov.Content); i += 2 {
			k := ov.Content[i]
			if ks, ok := scalar(k); ok {
				keys[ks] = true
			}
		}
		all := make([]string, 0, len(keys))
		for k := range keys {
			all = append(all, k)
		}
		sort.Strings(all)
		for _, k := range all {
			newPath := k
			if path != "" {
				newPath = path + "." + k
			}
			bv := yamlutil.MapGet(base, k)
			ovv := yamlutil.MapGet(ov, k)
			if bv == nil && ovv != nil {
				result[newPath] = decodeNode(ovv)
				continue
			}
			if bv != nil && ovv == nil {
				result[newPath] = map[string]any{"$delete": true}
				continue
			}
			if bv != nil && ovv != nil {
				a.deepCompare(bv, ovv, newPath, result)
			}
		}
		return
	case yaml.SequenceNode:
		if len(base.Content) != len(ov.Content) {
			result[path] = decodeNode(ov)
			return
		}
		for i := range base.Content {
			if i >= len(ov.Content) {
				break
			}
			a.deepCompare(base.Content[i], ov.Content[i], path+"["+string(rune('0'+i))+"]", result) // simple
		}
		if len(base.Content) != len(ov.Content) {
			// recheck simple
		}
		return
	case yaml.ScalarNode:
		if base.Value != ov.Value {
			result[path] = decodeNode(ov)
		}
		return
	default:
		return
	}
}

func scalar(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

func decodeNode(n *yaml.Node) any {
	v, _ := yamlutil.Decode(n)
	return v
}

func equalNodes(a, b *yaml.Node) bool {
	eq, _ := yamlutil.DeepEqual(a, b)
	return eq
}

func pathOrDollar(path string, base, ov *yaml.Node) string {
	if path == "" {
		return "$resource"
	}
	return path
}

// GetParameterizableDifferences returns differences.
func (a *OverlayAnalyzer) GetParameterizableDifferences() map[string]map[string]any {
	return a.Differences
}

// GetAnalysisSummary returns summary.
func (a *OverlayAnalyzer) GetAnalysisSummary() map[string]any {
	total := 0
	for _, ch := range a.Differences {
		total += len(ch)
	}
	params := make([]string, 0, len(a.Parameterizable))
	for p := range a.Parameterizable {
		params = append(params, p)
	}
	sort.Strings(params)
	db := make(map[string]int)
	ovNames := make([]string, 0, len(a.Differences))
	for n := range a.Differences {
		ovNames = append(ovNames, n)
	}
	sort.Strings(ovNames)
	for _, n := range ovNames {
		db[n] = len(a.Differences[n])
	}
	return map[string]any{
		"overlays_analyzed":      len(a.Differences),
		"total_differences":      total,
		"parameterizable_paths":  params,
		"differences_by_overlay": db,
	}
}
