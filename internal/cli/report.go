package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func displayMigrationReport(report map[string]any, format string) {
	switch format {
	case "json":
		out, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(out))
		return
	case "yaml":
		out, _ := yaml.Marshal(report)
		fmt.Print(string(out))
		return
	default:
		fmt.Println("Migration completed successfully")
		if v, ok := report["target_directory"].(string); ok {
			fmt.Println("Chart: " + v)
		}
		if v, ok := report["resources_migrated"].(int); ok {
			fmt.Printf("Resources: %d\n", v)
		} else if v, ok := report["resources_migrated"].(float64); ok {
			fmt.Printf("Resources: %d\n", int(v))
		}
		if v, ok := report["overlays_processed"].(int); ok {
			fmt.Printf("Overlays: %d\n", v)
		} else if v, ok := report["overlays_processed"].(float64); ok {
			fmt.Printf("Overlays: %d\n", int(v))
		}
		if v, ok := report["values_files_generated"].(int); ok {
			fmt.Printf("Values files: %d\n", v)
		} else if v, ok := report["values_files_generated"].(float64); ok {
			fmt.Printf("Values files: %d\n", int(v))
		}
		if v, ok := report["validation"].(string); ok {
			fmt.Println("Validation: " + v)
		}
		if w, ok := report["warnings"].([]string); ok {
			for _, x := range w {
				fmt.Fprintln(os.Stderr, "Warning: "+x)
			}
		}
	}
}

func displayAnalysisReport(report map[string]any, format string) {
	switch format {
	case "json":
		out, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(out))
		return
	case "yaml":
		out, _ := yaml.Marshal(report)
		fmt.Print(string(out))
		return
	default:
		if v, ok := report["source_directory"].(string); ok {
			fmt.Println("Source: " + v)
		}
		if v, ok := report["resources_found"].(int); ok {
			fmt.Printf("Resources: %d\n", v)
		} else if v, ok := report["resources_found"].(float64); ok {
			fmt.Printf("Resources: %d\n", int(v))
		}
		if v, ok := report["migration_complexity"].(string); ok {
			fmt.Println("Complexity: " + v)
		}
		if rt, ok := report["resource_types"].(map[string]int); ok {
			for k, c := range rt {
				fmt.Printf("  %s: %d\n", k, c)
			}
		}
		if w, ok := report["warnings"].([]string); ok {
			for _, x := range w {
				fmt.Fprintln(os.Stderr, "Warning: "+x)
			}
		}
	}
}

func saveReport(report map[string]any, path, format string) error {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	var buf bytes.Buffer
	switch format {
	case "json":
		out, _ := json.MarshalIndent(report, "", "  ")
		buf.Write(out)
		buf.WriteByte('\n')
	case "yaml":
		out, _ := yaml.Marshal(report)
		buf.Write(out)
	default:
		for k, v := range report {
			fmt.Fprintf(&buf, "%s: %v\n", k, v)
		}
	}
	return os.WriteFile(abs, buf.Bytes(), 0644)
}
