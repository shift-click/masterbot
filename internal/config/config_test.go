package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateAllowsCoreOnlyRuntimeWithoutIrisURLs(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Iris.Enabled = false
	cfg.Iris.WSURL = ""
	cfg.Iris.HTTPURL = ""

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRequiresIrisURLsWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Iris.Enabled = true
	cfg.Iris.WSURL = ""
	cfg.Iris.HTTPURL = ""

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when iris is enabled without URLs")
	}
}

func TestValidateAllowsMetricsWithoutAdminSurface(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Admin.MetricsEnabled = true
	cfg.Admin.PseudonymSecret = "secret"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRequiresAllowlistWhenAdminEnabled(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Admin.Enabled = true
	cfg.Admin.MetricsEnabled = true
	cfg.Admin.PseudonymSecret = "secret"
	cfg.Admin.AllowedEmails = nil

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when admin.enabled=true without allowlist")
	}
}

func TestValidateAllowsAudienceScopesWhenAdminEnabled(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Admin.Enabled = true
	cfg.Admin.MetricsEnabled = true
	cfg.Admin.PseudonymSecret = "secret"
	cfg.Admin.AllowedEmails = nil
	cfg.Admin.AudienceScopes = []AdminAudienceScope{
		{
			Email:    "partner@example.com",
			Role:     "partner",
			Tenants:  []string{"tenant-a"},
			Rooms:    []string{"room-hash-a"},
			Features: []string{"coin"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsInvalidAudienceRole(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Admin.AudienceScopes = []AdminAudienceScope{
		{
			Email: "x@example.com",
			Role:  "super",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid audience role")
	}
}

func TestConfigValidateRequiresBootstrapAdminWhenRuntimeDBEnabled(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Access.RuntimeDBPath = "data/access.db"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when runtime db is enabled without bootstrap admin")
	}
}

func TestConfigValidateAllowsStaticACLWithoutRuntimeDB(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Access.Rooms = []AccessRoomConfig{
		{ChatID: "room-1", AllowIntents: []string{"help.show"}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate static acl config: %v", err)
	}
}

func TestConfigValidateAllowsBootstrapSuperAdminUsersWithoutLegacyPair(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Access.RuntimeDBPath = "data/access.db"
	cfg.Access.BootstrapSuperAdminUsers = []string{"230782300", "5254628352871285439"}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate runtime acl with bootstrap super admin users: %v", err)
	}
}

func TestValidateRejectsUnsupportedStoreDriver(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Store.Driver = "supabase"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unsupported store driver")
	}
}

func TestConfigValidateAllowsAutoQueryBootstrapRooms(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.AutoQuery.Rooms = []AutoQueryRoomConfig{
		{ChatID: "room-1", Mode: "local-auto"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate auto_query rooms: %v", err)
	}
}

func TestConfigValidateRejectsInvalidAutoQueryMode(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.AutoQuery.DefaultPolicy.Mode = "turbo-auto"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid auto_query mode")
	}
}

func TestResolvedInstancesBackwardCompat(t *testing.T) {
	t.Parallel()

	cfg := defaultIrisConfig()
	cfg.Instances = nil // no explicit instances

	instances := cfg.ResolvedInstances()
	if len(instances) != 1 {
		t.Fatalf("expected 1 resolved instance, got %d", len(instances))
	}
	if instances[0].ID != "default" {
		t.Fatalf("expected id 'default', got %q", instances[0].ID)
	}
	if instances[0].WSURL != cfg.WSURL {
		t.Fatalf("expected WSURL %q, got %q", cfg.WSURL, instances[0].WSURL)
	}
}

func TestResolvedInstancesExplicit(t *testing.T) {
	t.Parallel()

	cfg := defaultIrisConfig()
	cfg.Instances = []IrisInstanceConfig{
		{ID: "main", WSURL: "ws://a:3000/ws", HTTPURL: "http://a:3000", RequestTimeout: 5 * time.Second, ReconnectMin: time.Second, ReconnectMax: 30 * time.Second, RoomWorkerEnabled: true, RoomWorkerCount: 8},
		{ID: "sub", WSURL: "ws://b:3000/ws", HTTPURL: "http://b:3000", RequestTimeout: 5 * time.Second, ReconnectMin: time.Second, ReconnectMax: 30 * time.Second, RoomWorkerEnabled: true, RoomWorkerCount: 8},
	}

	instances := cfg.ResolvedInstances()
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
}

func TestResolvedInstancesDisabledReturnsNil(t *testing.T) {
	t.Parallel()

	cfg := IrisConfig{Enabled: false}
	instances := cfg.ResolvedInstances()
	if instances != nil {
		t.Fatalf("expected nil instances when disabled, got %d", len(instances))
	}
}

func TestValidateRejectsDuplicateInstanceIDs(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Iris.Instances = []IrisInstanceConfig{
		{ID: "main", WSURL: "ws://a:3000/ws", HTTPURL: "http://a:3000", RequestTimeout: 5 * time.Second, ReconnectMin: time.Second, ReconnectMax: 30 * time.Second, RoomWorkerEnabled: true, RoomWorkerCount: 8},
		{ID: "main", WSURL: "ws://b:3000/ws", HTTPURL: "http://b:3000", RequestTimeout: 5 * time.Second, ReconnectMin: time.Second, ReconnectMax: 30 * time.Second, RoomWorkerEnabled: true, RoomWorkerCount: 8},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for duplicate instance IDs")
	}
}

func TestValidateRejectsInstanceMissingURL(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Iris.Instances = []IrisInstanceConfig{
		{ID: "main", WSURL: "", HTTPURL: "http://a:3000", RequestTimeout: 5 * time.Second, ReconnectMin: time.Second, ReconnectMax: 30 * time.Second, RoomWorkerEnabled: true, RoomWorkerCount: 8},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for instance missing ws_url")
	}
}

func TestValidateRejectsDuplicateInstanceIDAndInvalidTimeouts(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Iris.Instances = []IrisInstanceConfig{
		{
			ID:                "main",
			WSURL:             "ws://a:3000/ws",
			HTTPURL:           "http://a:3000",
			RequestTimeout:    0,
			ReconnectMin:      time.Second,
			ReconnectMax:      500 * time.Millisecond,
			RoomWorkerEnabled: true,
			RoomWorkerCount:   0,
		},
		{
			ID:                "main",
			WSURL:             "ws://b:3000/ws",
			HTTPURL:           "http://b:3000",
			RequestTimeout:    5 * time.Second,
			ReconnectMin:      time.Second,
			ReconnectMax:      30 * time.Second,
			RoomWorkerEnabled: true,
			RoomWorkerCount:   8,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for duplicate id and invalid instance limits")
	}
	joined := err.Error()
	for _, want := range []string{
		`duplicate id "main"`,
		"iris.instances[0].request_timeout must be greater than zero",
		"iris.instances[0].reconnect_max must be >= reconnect_min",
		"iris.instances[0].room_worker_count must be greater than zero",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in %q", want, joined)
		}
	}
}

func TestValidateRejectsLegacySharedLimits(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Iris.RequestTimeout = 0
	cfg.Iris.ReconnectMin = 0
	cfg.Iris.ReconnectMax = 0
	cfg.Iris.RoomWorkerCount = 0
	cfg.Iris.Instances = nil

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid shared iris limits")
	}
	joined := err.Error()
	for _, want := range []string{
		"iris.request_timeout must be greater than zero",
		"iris.reconnect_min must be greater than zero",
		"iris.room_worker_count must be greater than zero",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in %q", want, joined)
		}
	}
}
