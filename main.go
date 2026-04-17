package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ---------------------------------------------------------
// 全局配置常量
// ---------------------------------------------------------
var regRoots = []string{
	`SYSTEM\CurrentControlSet\Enum\HID`,
	`SYSTEM\CurrentControlSet\Enum\BTHENUM`,
}

// 触摸板硬件 ID 黑名单 (防止反转笔记本自带触摸板)
var touchpadBlacklist = []string{
	"SYNAPTICS", "ELAN", "ALPS", "VEN_SYN", "VEN_ELAN", "VEN_ALPS", "MSFT0001", "PNP0F13",
}

// 统计状态
type Stats struct {
	Success         int
	Skipped         int
	TouchpadIgnored int
	Restarted       int
}

func main() {
	// 1. 检查管理员权限，若无则尝试提权
	if !isElevated() {
		fmt.Println("[!] 需要 Administrator 权限，正在拉起 UAC...")
		runAsAdmin()
		return
	}

	fmt.Println("[*] 开始扫描系统鼠标设备...")
	stats := Stats{}

	// 2. 遍历并修改设备
	processDevices(&stats)

	// 3. 打印执行报告
	printReport(stats)

	// 防止终端一闪而过
	fmt.Println("\n按回车键退出程序...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// ---------------------------------------------------------
// 核心逻辑
// ---------------------------------------------------------

func processDevices(stats *Stats) {
	for _, baseRoot := range regRoots {
		// 打开 HID 或 BTHENUM 根节点
		rootKey, err := registry.OpenKey(registry.LOCAL_MACHINE, baseRoot, registry.READ)
		if err != nil {
			continue // 可能不存在 BTHENUM，忽略
		}
		defer rootKey.Close()

		vidNames, err := rootKey.ReadSubKeyNames(-1)
		if err != nil {
			continue
		}

		for _, vidName := range vidNames {
			vidPath := baseRoot + `\` + vidName
			vidKey, err := registry.OpenKey(registry.LOCAL_MACHINE, vidPath, registry.READ)
			if err != nil {
				continue
			}

			instanceNames, err := vidKey.ReadSubKeyNames(-1)
			vidKey.Close()
			if err != nil {
				continue
			}

			for _, instanceName := range instanceNames {
				instancePath := vidPath + `\` + instanceName
				fullDevID := strings.SplitN(baseRoot, `Enum\`, 2)[1] + `\` + vidName + `\` + instanceName

				// 读取设备属性进行过滤
				if !processSingleDevice(instancePath, fullDevID, stats) {
					continue
				}
			}
		}
	}
}

func processSingleDevice(instancePath, fullDevID string, stats *Stats) bool {
	instKey, err := registry.OpenKey(registry.LOCAL_MACHINE, instancePath, registry.READ)
	if err != nil {
		return false
	}
	defer instKey.Close()

	// 1. 检查是否为鼠标设备
	devClass, _, err := instKey.GetStringValue("Class")
	if err != nil || devClass != "Mouse" {
		return false
	}

	// 2. 获取并检查 HardwareID
	hwIDs, _, err := instKey.GetStringsValue("HardwareID")
	if err != nil || len(hwIDs) == 0 {
		return false
	}
	hwID := hwIDs[0]

	// 3. 拦截触摸板防误伤
	if isTouchpad(hwID) {
		// fmt.Printf("[-] 拦截触摸板: %s\n", hwID)
		stats.TouchpadIgnored++
		return false
	}

	// 4. 打开或创建 Device Parameters
	paramsPath := instancePath + `\Device Parameters`
	paramsKey, exist, err := registry.CreateKey(registry.LOCAL_MACHINE, paramsPath, registry.READ|registry.SET_VALUE)
	if err != nil {
		return false
	}
	defer paramsKey.Close()

	// exist 为 false 表示键是新建的，之前不存在
	_ = exist

	// 5. 检查幂等性
	currentVal, _, err := paramsKey.GetIntegerValue("FlipFlopWheel")
	if err == nil && currentVal == 1 {
		stats.Skipped++
		return true // 已经开启，无需修改
	}

	// 6. 写入新值开启自然滚动
	err = paramsKey.SetDWordValue("FlipFlopWheel", 1)
	if err != nil {
		fmt.Printf("[x] 修改失败: %s\n", hwID)
		return false
	}

	stats.Success++
	fmt.Printf("[+] 已修改 -> %s\n", hwID)

	// 7. 尝试热重载设备驱动
	if restartDevice(fullDevID) {
		stats.Restarted++
	}

	return true
}

// ---------------------------------------------------------
// 辅助函数：硬件过滤、系统调用
// ---------------------------------------------------------

func isTouchpad(hwID string) bool {
	hwIDUpper := strings.ToUpper(hwID)
	for _, keyword := range touchpadBlacklist {
		if strings.Contains(hwIDUpper, keyword) {
			return true
		}
	}
	return false
}

func restartDevice(fullDevID string) bool {
	// 使用 PnPUtil 重载设备，隐藏控制台输出
	cmd := exec.Command("pnputil", "/restart-device", fullDevID)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Run()
	return err == nil
}

func printReport(stats Stats) {
	fmt.Println("\n=============================================")
	fmt.Println(" 任务完成 | 执行报告 (Golang Native)")
	fmt.Println("=============================================")
	fmt.Printf(" [✓] 成功反转滚轮:     %d 个设备\n", stats.Success)
	fmt.Printf(" [✓] 成功热重载驱动:   %d 个设备\n", stats.Restarted)
	fmt.Printf(" [-] 已是自然滚动跳过: %d 个设备\n", stats.Skipped)
	fmt.Printf(" [!] 防误伤保护(触摸板): %d 个设备\n", stats.TouchpadIgnored)
	fmt.Println("=============================================")

	if stats.Success > 0 && stats.Restarted < stats.Success {
		fmt.Println(" 💡 提示：部分设备未能热重载，可能需要重新插拔鼠标生效。")
	} else if stats.Success > 0 && stats.Restarted == stats.Success {
		fmt.Println(" 💡 提示：设备热重载成功，滚动方向已即刻生效！")
	}
}

// ---------------------------------------------------------
// 权限控制 (UAC)
// ---------------------------------------------------------

func isElevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

func runAsAdmin() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("获取当前路径失败:", err)
		return
	}

	verb, _ := syscall.UTF16PtrFromString("runas")
	exePath, _ := syscall.UTF16PtrFromString(exe)
	cwd, _ := os.Getwd()
	cwdPath, _ := syscall.UTF16PtrFromString(cwd)

	// 调用 Windows API 发起 UAC 提权请求
	err = windows.ShellExecute(0, verb, exePath, nil, cwdPath, windows.SW_NORMAL)
	if err != nil {
		fmt.Println("UAC 提权失败或被用户取消:", err)
	}

	// 稍微延迟退出，防止系统调度异常
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}
