package platform

import "testing"

func TestGetProjectMetaReturnsGoRuntimeMetadata(t *testing.T) {
	meta := GetProjectMeta()

	if got := meta["name"]; got != ProjectName {
		t.Fatalf("project name = %q, want %q", got, ProjectName)
	}
	if got := meta["version"]; got != ProjectVersion {
		t.Fatalf("project version = %q, want %q", got, ProjectVersion)
	}
	if got := GetProjectVersion(); got != ProjectVersion {
		t.Fatalf("GetProjectVersion() = %q, want %q", got, ProjectVersion)
	}
}

func TestGetProjectMetaReturnsCopy(t *testing.T) {
	meta := GetProjectMeta()
	originalVersion := meta["version"]
	meta["version"] = "mutated"

	if got := GetProjectMeta()["version"]; got != originalVersion {
		t.Fatalf("cached project version was mutated through returned map: got %q, want %q", got, originalVersion)
	}
}

func TestParseProjectTomlHandlesTomllibStringForms(t *testing.T) {
	values := parseProjectToml([]byte(`
[tool.other]
name = "ignored"
version = "ignored"

[project]
name = '  gork-en  '
version = "  1.2.3  " # inline comment
description = "ignored"

[project.optional-dependencies]
version = "ignored"
`))

	if got, want := values["name"], "  gork-en  "; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := values["version"], "  1.2.3  "; got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
	if _, ok := values["description"]; ok {
		t.Fatalf("description should not be captured: %#v", values)
	}
}
