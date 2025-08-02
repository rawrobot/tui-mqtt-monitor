package mqtt

import (
    "strings"
)

// TruncateTopic truncates a topic to show only the last N levels
// Example: "A/B/C/D" with depth 2 returns "C/D"
func TruncateTopic(topic string, depth int) string {
    if depth <= 0 {
        return topic
    }
    
    parts := strings.Split(topic, "/")
    if len(parts) <= depth {
        return topic
    }
    
    return strings.Join(parts[len(parts)-depth:], "/")
}
