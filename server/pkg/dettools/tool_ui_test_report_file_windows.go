//go:build windows

package dettools

import (
	"os"

	"golang.org/x/sys/windows"
)

func openUITestEvidence(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY, 0)
}

func uiTestEvidenceLinkCount(file *os.File, _ os.FileInfo) (uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
}
