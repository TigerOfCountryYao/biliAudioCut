package extensions

import "testing"

func TestDisconnectGracePeriodIsLongerThanReconnectDelay(t *testing.T) {
	if disconnectGracePeriod <= 3_000_000_000 {
		t.Fatal("grace period must allow the extension reconnect delay")
	}
}
