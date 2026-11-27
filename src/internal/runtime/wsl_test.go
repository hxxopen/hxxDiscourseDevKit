package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestInstallRejectsUnknownToken(t *testing.T) {
	manager := &Manager{}
	err := manager.Install(context.Background(), "missing", nil)
	if !errors.Is(err, ErrInstallConfirm) {
		t.Fatalf("Install error = %v, want ErrInstallConfirm", err)
	}
}

func TestParseDistroList(t *testing.T) {
	output := "* Ubuntu-22.04\r\nDebian\r\n\r\nWindows Subsystem for Linux\r\nUbuntu-22.04\r\n"
	got := parseDistroList(output)
	want := []string{"Ubuntu-22.04", "Debian"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDistroList = %#v, want %#v", got, want)
	}

	noisy := "wsl: 检测到 localhost 代理配置，但未镜像到 WSL。NAT 模式下的 WSL 不支持 localhost 代理。\nUbuntu-24.04\n"
	if got := parseDistroList(noisy); !reflect.DeepEqual(got, []string{"Ubuntu-24.04"}) {
		t.Fatalf("parseDistroList(noisy) = %#v", got)
	}
}

func TestTrimWSLOutputDropsBridgeDiagnostics(t *testing.T) {
	const warning = "wsl: 检测到 localhost 代理配置，但未镜像到 WSL。NAT 模式下的 WSL 不支持 localhost 代理。"
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "warning before home",
			input: warning + "\n/home/fushou\n",
			want:  "/home/fushou",
		},
		{
			name:  "warning after uid",
			input: "0\n" + warning + "\r\n",
			want:  "0",
		},
		{
			name:  "windows newlines and nul",
			input: warning + "\r\n/home/dev\x00\r\n",
			want:  "/home/dev",
		},
		{
			name:  "no diagnostics",
			input: "Ubuntu-24.04\n",
			want:  "Ubuntu-24.04",
		},
		{
			name:  "empty",
			input: warning + "\n",
			want:  "",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := trimWSLOutput(test.input); got != test.want {
				t.Fatalf("trimWSLOutput(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestStripWSLProxyNoiseKeepsRealErrors(t *testing.T) {
	const warning = "wsl: 检测到 localhost 代理配置，但未镜像到 WSL。NAT 模式下的 WSL 不支持 localhost 代理。"
	genuine := "wsl: 无法启动分发版 Ubuntu-24.04 (退出代码 1)"
	got := stripWSLProxyNoise(warning + "\n" + genuine)
	if got != genuine {
		t.Fatalf("stripWSLProxyNoise = %q, want %q", got, genuine)
	}
}

func TestWSLScalar(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"0\n", "0"},
		{"banner\n/home/dev\n\n", "/home/dev"},
		{"\n\n", ""},
	}
	for _, test := range cases {
		if got := wslScalar(test.input); got != test.want {
			t.Errorf("wslScalar(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	output := `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
ID=debian
ID_LIKE=""
VERSION_ID="12"
DOUBLE='quoted value'
# comment
`
	got := parseOSRelease(output)
	if got["PRETTY_NAME"] != "Debian GNU/Linux 12 (bookworm)" {
		t.Errorf("PRETTY_NAME = %q", got["PRETTY_NAME"])
	}
	if got["ID"] != "debian" {
		t.Errorf("ID = %q", got["ID"])
	}
	if got["DOUBLE"] != "quoted value" {
		t.Errorf("DOUBLE = %q", got["DOUBLE"])
	}
}

func TestWSLUNCPath(t *testing.T) {
	got := WSLUNCPath("Ubuntu-22.04", "/home/dev/hxxwork")
	want := `\\wsl.localhost\Ubuntu-22.04\home\dev\hxxwork`
	if got != want {
		t.Fatalf("WSLUNCPath = %q, want %q", got, want)
	}
}

func TestWSLDataRoot(t *testing.T) {
	cases := []struct {
		home string
		user string
		want string
	}{
		{"/home/dev", "", "/home/dev/hxxwork"},
		{"/home/dev/", "", "/home/dev/hxxwork"},
		{"/root", "root", "/root/hxxwork"},
		{"", "dev", "/home/dev/hxxwork"},
		{"", "", ""},
		{"/home/", "", ""},
		{"wsl: 检测到 localhost 代理配置\n/home/dev", "", ""},
		{"", "dev\nwsl: noise", ""},
		{"relative/home", "", ""},
	}
	for _, test := range cases {
		if got := WSLDataRoot(test.home, test.user); got != test.want {
			t.Errorf("WSLDataRoot(%q, %q) = %q, want %q", test.home, test.user, got, test.want)
		}
	}
}

func TestToWSLPathRejectsControlCharacters(t *testing.T) {
	if _, err := ToWSLPath("\\\\wsl.localhost\\Ubuntu-24.04\\home\\dev\nhxxwork"); err == nil {
		t.Fatal("expected error for a path containing a newline")
	}
	got, err := ToWSLPath("\\\\wsl.localhost\\Ubuntu-24.04\\home\\dev\\hxxwork")
	if err != nil || got != "/home/dev/hxxwork" {
		t.Fatalf("ToWSLPath = %q, %v", got, err)
	}
}

func TestSortPostgresCandidates(t *testing.T) {
	input := []string{
		"/usr/lib/postgresql/9.6/bin/pg_ctl",
		"/usr/lib/postgresql/16/bin/pg_ctl",
		"/usr/lib/postgresql/15/bin/pg_ctl",
	}
	got := sortPostgresCandidates(input)
	want := []string{
		"/usr/lib/postgresql/16/bin/pg_ctl",
		"/usr/lib/postgresql/15/bin/pg_ctl",
		"/usr/lib/postgresql/9.6/bin/pg_ctl",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortPostgresCandidates = %#v, want %#v", got, want)
	}
}

func TestNormalizeInstallComponents(t *testing.T) {
	got, err := normalizeInstallComponents(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"postgresql", "redis", "git"}) {
		t.Fatalf("default components = %#v", got)
	}

	got, err = normalizeInstallComponents([]string{" Redis ", "redis", "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"redis", "postgresql"}) {
		t.Fatalf("normalized components = %#v", got)
	}

	if _, err := normalizeInstallComponents([]string{"mysql"}); err == nil {
		t.Fatal("expected unsupported component error")
	}
}

func TestIsWindowsInteropPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/mnt/c/Program Files/nodejs/npm", true},
		{"/mnt/d/work/project", true},
		{"/mnt/wsl/docker-desktop", false},
		{"/mnt/c", false},
		{"/usr/local/bin/node", false},
		{"/opt/node-v24.22.3-linux-x64/bin/node", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isWindowsInteropPath(tc.path); got != tc.want {
			t.Errorf("isWindowsInteropPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsDebianLike(t *testing.T) {
	cases := []struct {
		status WSLStatus
		want   bool
	}{
		{WSLStatus{OSID: "debian"}, true},
		{WSLStatus{OSID: "ubuntu"}, true},
		{WSLStatus{OSID: "linuxmint", OSLike: "ubuntu debian"}, true},
		{WSLStatus{OSID: "fedora", OSLike: "rhel"}, false},
	}
	for _, tc := range cases {
		if got := isDebianLike(tc.status); got != tc.want {
			t.Errorf("isDebianLike(%+v) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
