package remote

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestOperateIdentity(t *testing.T) {
	boolp := func(b bool) *bool { return &b }
	named := &ServerInfo{Name: "orders-prod"}
	noName := &ServerInfo{}
	declared := &ServerInfo{Name: "orders-prod", Production: boolp(true)}
	declaredFalse := &ServerInfo{Name: "orders-prod", Production: boolp(false)}

	for _, tc := range []struct {
		name     string
		info     *ServerInfo
		env      config.ResolvedEnv
		want     string
		wantProd bool
	}{
		{"cluster name wins over env name", named, config.ResolvedEnv{Name: "staging"}, "orders-prod", false},
		{"no cluster name falls back to env name", noName, config.ResolvedEnv{Name: "staging"}, "staging", false},
		{"no server info falls back to env name", nil, config.ResolvedEnv{Name: "staging"}, "staging", false},
		{"nothing known", nil, config.ResolvedEnv{}, "", false},
		{"server declares production", declared, config.ResolvedEnv{Name: "staging"}, "orders-prod", true},
		{"env opts in without server info", nil, config.ResolvedEnv{Name: "prod", Production: true}, "prod", true},
		{"env opt-in survives a non-declaring server", noName, config.ResolvedEnv{Name: "prod", Production: true}, "prod", true},
		{"both declare", declared, config.ResolvedEnv{Name: "prod", Production: true}, "orders-prod", true},
		{"env false never downgrades a declaring server", declared, config.ResolvedEnv{Name: "staging", Production: false}, "orders-prod", true},
		{"server false plus env false stays baseline", declaredFalse, config.ResolvedEnv{Name: "staging"}, "orders-prod", false},
		{"env opt-in overrides an explicit server false", declaredFalse, config.ResolvedEnv{Name: "staging", Production: true}, "orders-prod", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, prod := operateIdentity(tc.info, tc.env)
			if target != tc.want || prod != tc.wantProd {
				t.Errorf("operateIdentity(%+v, %+v) = (%q, %v), want (%q, %v)",
					tc.info, tc.env, target, prod, tc.want, tc.wantProd)
			}
		})
	}
}
