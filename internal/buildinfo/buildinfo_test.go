package buildinfo

import "testing"

func TestFromEnvUsesSafeDefaults(t *testing.T) {
	t.Setenv("JUCOBOT_BUILD_VERSION", "")
	t.Setenv("JUCOBOT_BUILD_REVISION", "")
	t.Setenv("JUCOBOT_BUILD_TIME", "")

	info := FromEnv()
	if info.Version != "dev" {
		t.Fatalf("version = %q, want dev", info.Version)
	}
	if info.Revision != "unknown" {
		t.Fatalf("revision = %q, want unknown", info.Revision)
	}
	if info.BuildTime != "unknown" {
		t.Fatalf("build time = %q, want unknown", info.BuildTime)
	}
}

func TestFromEnvUsesProvidedValues(t *testing.T) {
	t.Setenv("JUCOBOT_BUILD_VERSION", "20260317073008")
	t.Setenv("JUCOBOT_BUILD_REVISION", "abc1234")
	t.Setenv("JUCOBOT_BUILD_TIME", "2026-03-17T07:30:08Z")

	info := FromEnv()
	if info.Version != "20260317073008" {
		t.Fatalf("version = %q", info.Version)
	}
	if info.Revision != "abc1234" {
		t.Fatalf("revision = %q", info.Revision)
	}
	if info.BuildTime != "2026-03-17T07:30:08Z" {
		t.Fatalf("build time = %q", info.BuildTime)
	}
}
