//go:build windows

package backend

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	globalJob   windows.Handle
	jobInitOnce sync.Once
)

// initJobObject создаёт Job Object с флагом KILL_ON_JOB_CLOSE.
// Все дочерние процессы (sing-box, freeturnclient) привязываются к нему.
// При аварийном завершении GUI — все дочерние процессы автоматически убиваются.
func initJobObject() {
	jobInitOnce.Do(func() {
		var err error
		globalJob, err = windows.CreateJobObject(nil, nil)
		if err != nil {
			log.Printf("[PROC] Не удалось создать Job Object: %v", err)
			return
		}

		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		_, err = windows.SetInformationJobObject(
			globalJob,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		)
		if err != nil {
			log.Printf("[PROC] Не удалось настроить Job Object: %v", err)
			windows.CloseHandle(globalJob)
			globalJob = 0
		}
	})
}

// prepareProcessGroup настраивает SysProcAttr для скрытия окна и создания новой группы процессов.
func prepareProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// attachToJob привязывает запущенный процесс к глобальному Job Object.
func attachToJob(cmd *exec.Cmd) {
	initJobObject()
	if globalJob == 0 || cmd.Process == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		log.Printf("[PROC] OpenProcess для Job Object: %v", err)
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(globalJob, h); err != nil {
		log.Printf("[PROC] AssignProcessToJobObject: %v", err)
	}
}

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGenerateConsoleCtrlEvent = modkernel32.NewProc("GenerateConsoleCtrlEvent")
)

// signalStop отправляет CTRL_BREAK_EVENT процессу для graceful shutdown.
func signalStop(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("process is nil")
	}
	r, _, err := procGenerateConsoleCtrlEvent.Call(
		syscall.CTRL_BREAK_EVENT,
		uintptr(cmd.Process.Pid),
	)
	if r == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %w", err)
	}
	return nil
}
