package isomgr

type OSType string

const (
	OSTypeWindows OSType = "Windows"
	OSTypeLinux   OSType = "Linux"
	OSTypeWinPE   OSType = "WinPE"
	OSTypeUtility OSType = "Utility"
	OSTypeUnknown OSType = "Unknown"
)

type ISOInfo struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	SizeHR       string `json:"sizeHR"`
	OSType       OSType `json:"osType"`
	Arch         string `json:"arch"`
	Enabled      bool   `json:"enabled"`
	UnattendPath string `json:"unattendPath"`
}
