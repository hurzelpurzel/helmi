// Package templateconv provides compatibility helpers for callers that inspect
// parameterizable fields. Chart generation itself is fidelity-first and does not
// rewrite manifests, so this package performs only conservative extraction and
// never changes names, selectors, references, or custom-resource fields.
package templateconv

import (
	"helmchen/internal/resources"
	"helmchen/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

// Converter copies resources and exposes common fields by resource.
type Converter struct {
	ChartName string
}

// New creates a Converter.
func New(chartName string) *Converter {
	return &Converter{ChartName: chartName}
}

// ConvertResource returns a clean deep copy. Lossy implicit templating is avoided.
func (c *Converter) ConvertResource(resource *yaml.Node) *yaml.Node {
	if resource == nil {
		return nil
	}
	return yamlutil.Clone(resource)
}

// ExtractParameterizableValues extracts common workload fields per resource.
func (c *Converter) ExtractParameterizableValues(all []*yaml.Node) (map[string]any, error) {
	workloads := make(map[string]any)
	for _, r := range all {
		if r == nil {
			continue
		}
		values, err := c.extractResource(r)
		if err != nil {
			return nil, err
		}
		if len(values) == 0 {
			continue
		}
		key, err := resources.ResourceKey(r)
		if err != nil {
			return nil, err
		}
		workloads[key] = values
	}
	if len(workloads) == 0 {
		return map[string]any{}, nil
	}
	return map[string]any{"workloads": workloads}, nil
}

func (c *Converter) extractResource(r *yaml.Node) (map[string]any, error) {
	values := make(map[string]any)
	spec := yamlutil.MapGet(r, "spec")
	if spec != nil && !yamlutil.IsMapping(spec) {
		return values, nil
	}
	if replicas := yamlutil.MapGet(spec, "replicas"); replicas != nil {
		v, err := yamlutil.Decode(replicas)
		if err != nil {
			return nil, err
		}
		values["replicas"] = v
	}

	tmpl := yamlutil.MapGet(spec, "template")
	podSpec := yamlutil.MapGet(tmpl, "spec")
	containers := yamlutil.MapGet(podSpec, "containers")
	if yamlutil.IsSequence(containers) {
		var containerValues []any
		for _, cont := range containers.Content {
			if !yamlutil.IsMapping(cont) {
				continue
			}
			item := make(map[string]any)
			for _, field := range []string{"name", "image", "imagePullPolicy", "resources"} {
				if v := yamlutil.MapGet(cont, field); v != nil {
					dv, err := yamlutil.Decode(v)
					if err != nil {
						return nil, err
					}
					item[field] = dv
				}
			}
			if len(item) > 0 {
				containerValues = append(containerValues, item)
			}
		}
		if len(containerValues) > 0 {
			values["containers"] = containerValues
		}
	}

	kind := yamlutil.NodeString(yamlutil.MapGet(r, "kind"))
	if kind == "Service" {
		service := make(map[string]any)
		for _, field := range []string{"type", "ports"} {
			if v := yamlutil.MapGet(spec, field); v != nil {
				dv, err := yamlutil.Decode(v)
				if err != nil {
					return nil, err
				}
				service[field] = dv
			}
		}
		values["service"] = service
	}
	if kind == "ConfigMap" || kind == "Secret" {
		for _, field := range []string{"data", "stringData", "binaryData", "type"} {
			if v := yamlutil.MapGet(r, field); v != nil {
				dv, err := yamlutil.Decode(v)
				if err != nil {
					return nil, err
				}
				values[field] = dv
			}
		}
	}
	return values, nil
}
