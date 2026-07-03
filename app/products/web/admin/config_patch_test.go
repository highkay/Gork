package admin

import "testing"

func TestCloneAdminMapDeepCopiesNestedValues(t *testing.T) {
	original := map[string]any{
		"proxy": map[string]any{"url": "https://proxy.test"},
		"sso": map[string]any{
			"items": []any{map[string]any{"token": "before"}},
		},
	}

	cloned := cloneAdminMap(original)
	original["proxy"].(map[string]any)["url"] = "https://changed.test"
	original["sso"].(map[string]any)["items"].([]any)[0].(map[string]any)["token"] = "after"

	if got := cloned["proxy"].(map[string]any)["url"]; got != "https://proxy.test" {
		t.Fatalf("cloned proxy url = %v", got)
	}
	if got := cloned["sso"].(map[string]any)["items"].([]any)[0].(map[string]any)["token"]; got != "before" {
		t.Fatalf("cloned sso token = %v", got)
	}
}
