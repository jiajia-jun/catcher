package version

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "1.0.0", want: "v1.0.0"},
		{in: "v1.0.0", want: "v1.0.0"},
		{in: "V1.0.0", want: "v1.0.0"},
		{in: " 1.1.0-dev ", want: "v1.1.0-dev"},
		{in: "v1.1.0-rc.1+meta", want: "v1.1.0-rc.1+meta"},
		{in: "", wantErr: true},
		{in: "1.0", wantErr: true},
		{in: "release-1.0.0", wantErr: true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) 期望报错，实际得到 %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) 返回错误: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDisplay(t *testing.T) {
	cases := map[string]string{
		"v1.2.3":     "1.2.3",
		"1.2.3":      "1.2.3",
		"1.2.3-rc.1": "1.2.3-rc.1",
		"not-semver": "not-semver",
	}
	for in, want := range cases {
		if got := Display(in); got != want {
			t.Errorf("Display(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{current: "1.0.0", latest: "v1.1.0", want: true},
		{current: "1.1.0", latest: "v1.1.0", want: false},
		{current: "1.1.1", latest: "v1.1.0", want: false},
		{current: "1.1.0-dev", latest: "v1.1.0", want: true},
		{current: "1.1.0", latest: "v1.1.0-rc.1", want: false},
		{current: "1.9.0", latest: "v1.10.0", want: true},
	}
	for _, c := range cases {
		got, err := IsNewer(c.current, c.latest)
		if err != nil {
			t.Errorf("IsNewer(%q, %q) 返回错误: %v", c.current, c.latest, err)
			continue
		}
		if got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestIsNewerRejectsInvalid(t *testing.T) {
	if _, err := IsNewer("dev", "v1.0.0"); err == nil {
		t.Error("当前版本非法时应报错")
	}
	if _, err := IsNewer("1.0.0", "latest"); err == nil {
		t.Error("最新版本非法时应报错")
	}
}
