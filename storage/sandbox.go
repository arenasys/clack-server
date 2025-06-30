package storage

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
)

func findNobody() *user.User {
	if usr, err := user.Lookup("nobody"); err == nil {
		return usr
	}

	if usr, err := user.Current(); err == nil {
		return usr
	}

	return nil
}

func canSandbox() bool {
	if _, err := exec.LookPath("nsjail"); err != nil {
		return false
	}
	if findNobody() == nil {
		return false
	}
	return true
}

func sandboxFFmpegCommand(tmpPath string, args ...string) (*exec.Cmd, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found")
	}

	nobody := findNobody()
	if nobody == nil {
		return nil, fmt.Errorf("could not find nobody user")
	}

	pwd, _ := os.Getwd()
	nsArgs := []string{
		"-Mo",
		"--user", nobody.Uid, "--group", nobody.Gid,

		"--bindmount_ro", fmt.Sprintf("%s/ffmpeg:/ffmpeg", pwd),
		"--bindmount_ro", "/dev/urandom:/dev/urandom",

		"--disable_proc",
		"--iface_no_lo",

		"--rlimit_as", "1024",
		"--rlimit_core", "0",
		"--rlimit_cpu", "10",
		"--rlimit_fsize", "0",
		"--rlimit_nofile", "128",
		"--rlimit_nproc", "128",
		"--rlimit_stack", "8",
		"--rlimit_memlock", "64",
		"--rlimit_rtprio", "0",
		"--rlimit_msgqueue", "0",

		"--seccomp_string", `KILL_PROCESS {
			ptrace,
			process_vm_readv,
			process_vm_writev
		},
		ERRNO(1) {
			socket,
			sched_setaffinity
		}
		DEFAULT ALLOW`,
	}

	if tmpPath != "" {
		nsArgs = append(nsArgs, []string{
			"--bindmount_ro", tmpPath + ":" + tmpPath,
		}...)
	}

	nsArgs = append(nsArgs, []string{
		"--", "ffmpeg",
	}...)

	nsArgs = append(nsArgs, args...)

	cmd := exec.Command("nsjail", nsArgs...)
	return cmd, nil
}
