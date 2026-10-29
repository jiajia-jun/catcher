package entity

import "testing"

func TestPlatformExperimental(t *testing.T) {
	cases := map[Platform]bool{
		PlatformYuketang:  true,
		PlatformPigai:     true,
		PlatformChaoxing:  false,
		PlatformTronclass: false,
	}
	for p, want := range cases {
		if got := p.Experimental(); got != want {
			t.Errorf("%s.Experimental() = %v, want %v", p, got, want)
		}
	}
	// 未知平台不标实验性，避免把拼错的值当成已知平台
	if Platform("unknown").Experimental() {
		t.Fatal("未知平台不应标实验性")
	}
}
