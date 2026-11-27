//go:build windows

package secret

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWSLHostPath(t *testing.T) {
	cases := []struct {
		path   string
		distro string
		linux  string
		ok     bool
	}{
		{
			path:   `\\wsl.localhost\Ubuntu-24.04\home\dev\hxxwork\services\redis\redis.conf`,
			distro: "Ubuntu-24.04",
			linux:  "/home/dev/hxxwork/services/redis/redis.conf",
			ok:     true,
		},
		{
			path:   `\\wsl$\Debian\root\.cache\secrets.json`,
			distro: "Debian",
			linux:  "/root/.cache/secrets.json",
			ok:     true,
		},
		{path: `C:\DevKit\config\secrets.json`, ok: false},
		{path: `\\server\share\secrets.json`, ok: false},
		{path: `\\wsl.localhost\Ubuntu-24.04`, ok: false},
	}
	for _, test := range cases {
		distro, linux, ok := wslHostPath(test.path)
		if ok != test.ok || distro != test.distro || linux != test.linux {
			t.Errorf("wslHostPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
				test.path, distro, linux, ok, test.distro, test.linux, test.ok)
		}
	}
}

func TestProtectFileRestrictsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := protectFile(path); err != nil {
		t.Fatalf("protectFile: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo: %v", err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatalf("Control: %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL is not protected: control=%#x", control)
	}

	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatalf("Owner: %v", err)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatalf("OpenCurrentProcessToken: %v", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatalf("GetTokenUser: %v", err)
	}
	if !owner.Equals(user.User.Sid) {
		t.Fatalf("owner = %s, want current user %s", owner.String(), user.User.Sid.String())
	}
}
