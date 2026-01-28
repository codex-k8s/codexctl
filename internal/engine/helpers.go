package engine

func normalizeMapSlice(value any) []map[string]any {
	if value == nil {
		return nil
	}
	var result []map[string]any
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
	case []map[string]any:
		result = append(result, v...)
	}
	return result
}
