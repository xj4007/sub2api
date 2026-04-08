package setup

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestDecideAdminBootstrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		totalUsers int64
		adminUsers int64
		should     bool
		reason     string
	}{
		{
			name:       "empty database should create admin",
			totalUsers: 0,
			adminUsers: 0,
			should:     true,
			reason:     adminBootstrapReasonEmptyDatabase,
		},
		{
			name:       "admin exists should skip",
			totalUsers: 10,
			adminUsers: 1,
			should:     false,
			reason:     adminBootstrapReasonAdminExists,
		},
		{
			name:       "users exist without admin should skip",
			totalUsers: 5,
			adminUsers: 0,
			should:     false,
			reason:     adminBootstrapReasonUsersExistWithoutAdmin,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideAdminBootstrap(tc.totalUsers, tc.adminUsers)
			if got.shouldCreate != tc.should {
				t.Fatalf("shouldCreate=%v, want %v", got.shouldCreate, tc.should)
			}
			if got.reason != tc.reason {
				t.Fatalf("reason=%q, want %q", got.reason, tc.reason)
			}
		})
	}
}

func TestSetupDefaultAdminConcurrency(t *testing.T) {
	t.Run("simple mode admin uses higher concurrency", func(t *testing.T) {
		t.Setenv("RUN_MODE", "simple")
		if got := setupDefaultAdminConcurrency(); got != simpleModeAdminConcurrency {
			t.Fatalf("setupDefaultAdminConcurrency()=%d, want %d", got, simpleModeAdminConcurrency)
		}
	})

	t.Run("standard mode keeps existing default", func(t *testing.T) {
		t.Setenv("RUN_MODE", "standard")
		if got := setupDefaultAdminConcurrency(); got != defaultUserConcurrency {
			t.Fatalf("setupDefaultAdminConcurrency()=%d, want %d", got, defaultUserConcurrency)
		}
	})
}

func TestWriteConfigFileKeepsDefaultUserConcurrency(t *testing.T) {
	t.Setenv("RUN_MODE", "simple")
	t.Setenv("DATA_DIR", t.TempDir())

	if err := writeConfigFile(&SetupConfig{}); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	data, err := os.ReadFile(GetConfigFilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(data), "user_concurrency: 5") {
		t.Fatalf("config missing default user concurrency, got:\n%s", string(data))
	}
}

func TestBuildPostgresDSN(t *testing.T) {
	cfg := &DatabaseConfig{
		Host:               "pgproxy",
		Port:               5432,
		User:               "sub2api",
		Password:           "secret",
		DBName:             "sub2api",
		SSLMode:            "disable",
		TargetSessionAttrs: "read-write",
	}

	defaultDSN := buildPostgresDSN(cfg, "postgres")
	if !strings.Contains(defaultDSN, "dbname=postgres") {
		t.Fatalf("default DSN should connect to postgres database, got %q", defaultDSN)
	}
	if !strings.Contains(defaultDSN, "target_session_attrs=read-write") {
		t.Fatalf("default DSN should keep target_session_attrs, got %q", defaultDSN)
	}

	targetDSN := buildPostgresDSN(cfg, cfg.DBName)
	if !strings.Contains(targetDSN, "dbname=sub2api") {
		t.Fatalf("target DSN should connect to target database, got %q", targetDSN)
	}
}

func TestBuildRedisUniversalOptionsUsesSentinel(t *testing.T) {
	cfg := &RedisConfig{
		Host:               "localhost",
		Port:               6379,
		Password:           "secret",
		DB:                 2,
		EnableTLS:          true,
		SentinelEnabled:    true,
		SentinelMasterName: "sub2api-redis",
		SentinelAddrs:      "10.0.0.1:26379,10.0.0.2:26379,10.0.0.3:26379",
	}

	opts := buildRedisUniversalOptions(cfg)
	if opts.MasterName != "sub2api-redis" {
		t.Fatalf("MasterName=%q", opts.MasterName)
	}
	if len(opts.Addrs) != 3 {
		t.Fatalf("Addrs=%v", opts.Addrs)
	}
	if opts.Addrs[0] != "10.0.0.1:26379" || opts.Addrs[2] != "10.0.0.3:26379" {
		t.Fatalf("Addrs=%v", opts.Addrs)
	}
	if opts.DB != 2 || opts.Password != "secret" {
		t.Fatalf("unexpected redis options: %#v", opts)
	}
	if opts.TLSConfig == nil {
		t.Fatalf("expected TLS config in sentinel mode")
	}

	client := buildRedisClient(cfg)
	if client == nil {
		t.Fatalf("expected redis client")
	}
	_ = client.Close()

	standalone := buildRedisUniversalOptions(&RedisConfig{Host: "redis", Port: 6379, Password: "pw", DB: 1})
	if standalone.MasterName != "" {
		t.Fatalf("standalone options should not set master name")
	}
	if len(standalone.Addrs) != 1 || standalone.Addrs[0] != "redis:6379" {
		t.Fatalf("standalone addrs=%v", standalone.Addrs)
	}
	if standalone.DialTimeout != 5*time.Second {
		t.Fatalf("standalone dial timeout=%v", standalone.DialTimeout)
	}
}

func TestBuildRedisClientReturnsUniversalClient(t *testing.T) {
	client := buildRedisClient(&RedisConfig{Host: "redis", Port: 6379})
	if _, ok := client.(redis.UniversalClient); !ok {
		t.Fatalf("expected universal client implementation")
	}
	_ = client.Close()
}
