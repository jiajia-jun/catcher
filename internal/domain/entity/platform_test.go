package entity

import "testing"

func TestPlatformExperimental(t *testing.T) {
	cases := map[Platform]bool{
		PlatformYuketang:  true,
		PlatformPigai:     true,
		PlatformChaoxing:  false,
		PlatformTronclass: false,
		PlatformFiforal:   false,
	}
	for p, want := range cases {
		if got := p.Experimental(); got != want {
			t.Errorf("%s.Experimental() = %v, want %v", p, got, want)
		}
	}
	// 未知平台按非实验性处理，拼错的值同样落到这个分支
	if Platform("unknown").Experimental() {
		t.Fatal("未知平台不应标实验性")
	}
}
