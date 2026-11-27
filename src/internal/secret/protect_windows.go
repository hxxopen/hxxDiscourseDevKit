//go:build windows

package secret

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// protectFile replaces the secrets file DACL with a protected DACL granting
// full control to the current user only. This closes the inherited access that
// a default file under %LOCALAPPDATA% would otherwise give to other local
// accounts.
func protectFile(path string) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("open process token: %w", err)
	}
	defer token.Close()

	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("resolve current user: %w", err)
	}
	if user.User.Sid == nil {
		return fmt.Errorf("current user has no SID")
	}

	sddl := fmt.Sprintf("D:P(A;;FA;;;%s)", user.User.Sid.String())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read security descriptor DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("set file DACL: %w", err)
	}
	return nil
}
