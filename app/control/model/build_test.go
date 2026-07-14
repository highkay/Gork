package model

import "testing"

func TestBuildSpecFromName(t *testing.T) {
	spec, ok := BuildSpecFromName("build/grok-4")
	if !ok {
		t.Fatal("expected ok")
	}
	if !spec.IsBuildChat() || spec.ModeID != ModeBuild || spec.ModelName != "build/grok-4" {
		t.Fatalf("spec=%#v", spec)
	}
	if UpstreamIDFromBuildModel("build/grok-4") != "grok-4" {
		t.Fatal("upstream")
	}
	if IsBuildModelName("grok-build-console") {
		t.Fatal("console name must not be build prefix")
	}
	if _, ok := BuildSpecFromName("build/"); ok {
		t.Fatal("empty upstream")
	}
	// Resolve 应命中 build 前缀
	got, err := Resolve("build/grok-4.5")
	if err != nil || !got.IsBuildChat() {
		t.Fatalf("Resolve build: %#v %v", got, err)
	}
}
