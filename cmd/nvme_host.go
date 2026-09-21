package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
	"truenas/truenas_incus_ctl/core"
)

// Локалната страна на NVMe-oF: свързване, намиране на устройството, разкачане.
//
// Разликите спрямо iSCSI, всяка от които е причина този файл да съществува отделно:
//
//   - няма userspace демон. iSCSI иска `iscsid` да върви; при NVMe работата я вършат
//     модулите `nvme-tcp`/`nvme-fabrics` в ядрото.
//   - устройството НЕ се появява в /dev/disk/by-path с предвидимо име. Пътят се намира
//     през sysfs: /sys/class/nvme-subsystem/*/subsysnqn се сверява с NQN-а, а блоковото
//     устройство е дете на съвпадналия. Същата логика е в incus-nvme-initiator.sh и е
//     доказана по нодовете.
//   - разкачането е по NQN, не по портал+таргет.

const nvmeSubsystemsDir = "/sys/class/nvme-subsystem"

func CheckNvmeCliExists() error {
	if _, err := exec.LookPath("nvme"); err != nil {
		return fmt.Errorf("nvme-cli is not installed or not in PATH, install the \"nvme-cli\" package")
	}
	return nil
}

func requireRootForNvme(action string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	if u.Uid != "0" {
		return fmt.Errorf("%s requires root privileges", action)
	}
	return nil
}

// IterateConnectedNvmeSubsystems обхожда свързаните subsystem-и и подава NQN-а с
// блоковите устройства под него.
//
// Това е NVMe заместителят на IterateActivatedIscsiShares. При iSCSI списъкът се чете от
// /dev/disk/by-path, където името носи портала и IQN-а. При NVMe такъв възел няма —
// същото се разбира само от sysfs.
func IterateConnectedNvmeSubsystems(cb func(subNqn string, devPaths []string)) {
	subsystems, err := os.ReadDir(nvmeSubsystemsDir)
	if err != nil {
		return
	}

	for _, sub := range subsystems {
		raw, err := os.ReadFile(filepath.Join(nvmeSubsystemsDir, sub.Name(), "subsysnqn"))
		if err != nil {
			continue
		}
		subNqn := strings.TrimSpace(string(raw))

		entries, err := os.ReadDir(filepath.Join(nvmeSubsystemsDir, sub.Name()))
		if err != nil {
			continue
		}

		devices := make([]string, 0, 1)
		for _, e := range entries {
			name := e.Name()
			// Namespace-ите са деца на subsystem-а и се казват nvmeXnY. Контролерите
			// (nvmeX) също са деца — разликата е точно във втората "n".
			if !strings.HasPrefix(name, "nvme") || !strings.Contains(name[4:], "n") {
				continue
			}
			devPath := "/dev/" + name
			if info, err := os.Stat(devPath); err == nil && info.Mode()&os.ModeDevice != 0 {
				devices = append(devices, devPath)
			}
		}

		cb(subNqn, devices)
	}
}

// FindNvmeDeviceBySubNqn връща /dev/nvmeXnY за даден subsystem NQN, или "" ако го няма.
func FindNvmeDeviceBySubNqn(subNqn string) string {
	found := ""
	IterateConnectedNvmeSubsystems(func(nqn string, devPaths []string) {
		if found != "" || nqn != subNqn || len(devPaths) == 0 {
			return
		}
		found = devPaths[0]
	})
	return found
}

func RunNvmeConnect(addr string, port int, subNqn string) error {
	if err := CheckNvmeCliExists(); err != nil {
		return err
	}
	args := []string{
		"connect",
		"-t", "tcp",
		"-a", stripIpV6Brackets(addr),
		"-s", fmt.Sprint(port),
		"-n", subNqn,
	}
	out, err, status := core.RunCommand("nvme", args...)
	if err != nil {
		// Вече свързан subsystem не е грешка — командата е идемпотентна по замисъл.
		if strings.Contains(strings.ToLower(out), "already connected") {
			return nil
		}
		return fmt.Errorf("nvme connect failed (%d): %v", status, err)
	}
	return nil
}

func RunNvmeDisconnect(subNqn string) error {
	if err := CheckNvmeCliExists(); err != nil {
		return err
	}
	out, err, status := core.RunCommand("nvme", "disconnect", "-n", subNqn)
	if err != nil {
		lower := strings.ToLower(out)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "no controllers") {
			return nil
		}
		return fmt.Errorf("nvme disconnect failed (%d): %v", status, err)
	}
	return nil
}

// WaitForNvmeDevice изчаква устройството да се появи след connect.
//
// Ядрото връща управлението преди namespace-ът да е разгледан, затова между успешен
// connect и наличен /dev/nvmeXnY минава време. Без изчакване следващата стъпка получава
// „няма такъв файл" и всичко изглежда като провалено свързване.
func WaitForNvmeDevice(subNqn string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for {
		if dev := FindNvmeDeviceBySubNqn(subNqn); dev != "" {
			return dev
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// WaitForNvmeDeviceGone е обратното — за deactivate --wait.
func WaitForNvmeDeviceGone(subNqn string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if FindNvmeDeviceBySubNqn(subNqn) == "" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// RunNvmeDiscover пита портала какви subsystem-и предлага.
//
// Това е NVMe съответствието на `iscsiadm -m discovery`, с което `share iscsi test`
// доказва, че порталът отговаря. Без него проверката на връзката при NVMe оставаше на
// iSCSI и буквално изискваше iSCSI услугата да е пусната на уреда — при положение че по
// нея не минава нито един байт.
func RunNvmeDiscover(addr string, port int) (string, error) {
	if err := CheckNvmeCliExists(); err != nil {
		return "", err
	}
	out, err, status := core.RunCommand("nvme", "discover",
		"-t", "tcp",
		"-a", stripIpV6Brackets(addr),
		"-s", fmt.Sprint(port),
	)
	if err != nil {
		return out, fmt.Errorf("nvme discover failed (%d): %v", status, err)
	}
	return out, nil
}

// RunNvmeRescan пресканира namespace-ите на свързаните контролери.
//
// Това е NVMe съответствието на `iscsiadm -m node -R` — начинът, по който порасналият
// том се вижда от нода, без да се разкача. Контролерите са /sys/class/nvme/nvmeX;
// `nvme ns-rescan` се пуска срещу символното устройство на всеки от тях.
func RunNvmeRescan() error {
	if err := CheckNvmeCliExists(); err != nil {
		return err
	}

	entries, err := os.ReadDir("/sys/class/nvme")
	if err != nil {
		// Няма нито един NVMe контролер — няма какво да се пресканира.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var firstErr error
	for _, e := range entries {
		name := e.Name()
		// Само контролери (nvmeX), не namespace-и (nvmeXnY).
		if !strings.HasPrefix(name, "nvme") || strings.Contains(name[4:], "n") {
			continue
		}
		if _, err, status := core.RunCommand("nvme", "ns-rescan", "/dev/"+name); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("nvme ns-rescan %s failed (%d): %v", name, status, err)
		}
	}
	return firstErr
}
