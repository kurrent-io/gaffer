package engine

import (
	"fmt"
	"strings"
)

func GenerateSource(source, partition string, emit bool) (string, error) {
	var sb strings.Builder

	switch {
	case strings.HasPrefix(source, "stream:"):
		name := escapeJS(strings.TrimPrefix(source, "stream:"))
		fmt.Fprintf(&sb, "fromStream('%s')\n", name)
	case strings.HasPrefix(source, "category:"):
		name := escapeJS(strings.TrimPrefix(source, "category:"))
		fmt.Fprintf(&sb, "fromCategory('%s')\n", name)
	case source == "all":
		sb.WriteString("fromAll()\n")
	default:
		return "", fmt.Errorf("unsupported source: %q (use 'all', 'stream:name', or 'category:name')", source)
	}

	switch partition {
	case "per-stream":
		sb.WriteString("  .foreachStream()\n")
	case "none":
		// no partitioning
	default:
		return "", fmt.Errorf("unsupported partition: %q (use 'none' or 'per-stream')", partition)
	}

	sb.WriteString("  .when({\n")
	sb.WriteString("    $init: function() {\n")
	sb.WriteString("      return {};\n")
	sb.WriteString("    },\n")
	sb.WriteString("    // Add your event handlers here\n")
	sb.WriteString("    // EventType: function(state, event) {\n")

	if emit {
		sb.WriteString("    //   emit('stream-name', 'EmittedType', { data: event.data });\n")
	}

	sb.WriteString("    //   return state;\n")
	sb.WriteString("    // }\n")
	sb.WriteString("  })\n")

	return sb.String(), nil
}

func escapeJS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
