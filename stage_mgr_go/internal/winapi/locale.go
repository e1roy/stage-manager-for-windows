//go:build windows

package winapi

var procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

// UILanguage returns the language identifier of the user's display
// language, the one Windows itself shows its words in.
func UILanguage() uint16 {
	id, _, _ := procGetUserDefaultUILanguage.Call()
	return uint16(id)
}
