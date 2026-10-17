package routes_test

// Convert explicitly open request DTOs to a closed validation view. No changes
// are made to the source schema; response/document validation remains closed.
func openAPIRequestClosedSchema(schema map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range schema {
		out[k] = v
	}
	if schema["additionalProperties"] == true && !openAPIA8FreeformMap(schema) {
		out["additionalProperties"] = false
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		cp := map[string]any{}
		for k, v := range props {
			if child, ok := v.(map[string]any); ok {
				cp[k] = openAPIRequestClosedSchema(child)
			} else {
				cp[k] = v
			}
		}
		out["properties"] = cp
	}
	if child, ok := schema["items"].(map[string]any); ok {
		out["items"] = openAPIRequestClosedSchema(child)
	}
	return out
}
func openAPIRequestKnownValue(schema map[string]any, value any) any {
	if openAPIA8FreeformMap(schema) {
		return value
	}
	switch v := value.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		out := map[string]any{}
		for k, item := range v {
			if child, ok := props[k].(map[string]any); ok {
				out[k] = openAPIRequestKnownValue(child, item)
			} else if schema["additionalProperties"] != true {
				out[k] = item
			}
		}
		return out
	case []any:
		child, _ := schema["items"].(map[string]any)
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = openAPIRequestKnownValue(child, item)
		}
		return out
	}
	return value
}
