package kernel

import (
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/config"
)

func TestPreviewSecretRules(t *testing.T) {
	long := strings.Repeat("s", 32)
	for _, tc := range []struct {
		name, env, secret string
		wantErr           string
	}{
		{"dev derives", "dev", "", ""},
		{"dev explicit", "dev", long, ""},
		{"dev too short", "dev", long[:31], "at least 32"},
		{"prod explicit", "prod", long, ""},
		{"prod unset", "prod", "", "required outside"},
		{"prod too short", "prod", "short", "at least 32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load(config.Options{Dir: t.TempDir(), Env: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			s := &Services{Config: cfg, Core: CoreConfig{Delivery: DeliveryConfig{PreviewSecret: tc.secret}}}
			got, err := previewSecret(s)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || len(got) < 32 {
				t.Fatalf("secret %d bytes, err %v", len(got), err)
			}
		})
	}
}
