package envvar

import "testing"

func TestIsTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "True", "yes", "YES", "on", "ON", "  on  ", "\ttrue\n"} {
		if !IsTruthy(v) {
			t.Errorf("IsTruthy(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "False", "no", "off", "OFF", "anything-else", "2", "yes please"} {
		if IsTruthy(v) {
			t.Errorf("IsTruthy(%q) = true, want false", v)
		}
	}
}
