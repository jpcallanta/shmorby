package tools

import (
	"testing"
)

// TestCheckMutating_Rm_Blocked checks rm is blocked.
func TestCheckMutating_Rm_Blocked(t *testing.T) {
	err := CheckMutating("rm -rf /tmp/x")
	if err == nil {
		t.Fatal("want error for rm, got nil")
	}
}

// TestCheckMutating_Mv_Blocked checks mv is blocked.
func TestCheckMutating_Mv_Blocked(t *testing.T) {
	err := CheckMutating("mv /tmp/a /tmp/b")
	if err == nil {
		t.Fatal("want error for mv, got nil")
	}
}

// TestCheckMutating_Dd_Blocked checks dd is blocked.
func TestCheckMutating_Dd_Blocked(t *testing.T) {
	err := CheckMutating("dd if=/dev/zero of=/tmp/out count=1")
	if err == nil {
		t.Fatal("want error for dd, got nil")
	}
}

// TestCheckMutating_Mkfs_Blocked checks mkfs is blocked.
func TestCheckMutating_Mkfs_Blocked(t *testing.T) {
	err := CheckMutating("mkfs.ext4 /dev/sdb1")
	if err == nil {
		t.Fatal("want error for mkfs, got nil")
	}
}

// TestCheckMutating_PkgInstall_Blocked checks package install is
// blocked.
func TestCheckMutating_PkgInstall_Blocked(t *testing.T) {
	err := CheckMutating("apt-get install nginx")
	if err == nil {
		t.Fatal("want error for apt-get install, got nil")
	}
}

// TestCheckMutating_SystemctlRestart_Blocked checks systemctl restart
// is blocked.
func TestCheckMutating_SystemctlRestart_Blocked(t *testing.T) {
	err := CheckMutating("systemctl restart nginx")
	if err == nil {
		t.Fatal("want error for systemctl restart, got nil")
	}
}

// TestCheckMutating_SystemctlStatus_Allowed checks systemctl status is
// allowed.
func TestCheckMutating_SystemctlStatus_Allowed(t *testing.T) {
	err := CheckMutating("systemctl status nginx")
	if err != nil {
		t.Errorf("want nil for systemctl status, got %v", err)
	}
}

// TestCheckMutating_Ls_Allowed checks ls is allowed.
func TestCheckMutating_Ls_Allowed(t *testing.T) {
	err := CheckMutating("ls -la /tmp")
	if err != nil {
		t.Errorf("want nil for ls, got %v", err)
	}
}

// TestCheckMutating_EtcRedirect_Blocked checks redirect to /etc is
// blocked.
func TestCheckMutating_EtcRedirect_Blocked(t *testing.T) {
	err := CheckMutating("echo config > /etc/foo.conf")
	if err == nil {
		t.Fatal("want error for redirect to /etc, got nil")
	}
}

// TestCheckMutating_CompoundCommand_Blocked checks rm after ; is
// blocked.
func TestCheckMutating_CompoundCommand_Blocked(t *testing.T) {
	err := CheckMutating("echo ok; rm -f /tmp/x")
	if err == nil {
		t.Fatal("want error for compound rm, got nil")
	}
}

// TestCheckMutating_CompoundCommandAllowed_NoMatch checks allowed
// compound with allowed verbs.
func TestCheckMutating_CompoundCommandAllowed_NoMatch(t *testing.T) {
	err := CheckMutating("ls -la; cat /etc/hosts")
	if err != nil {
		t.Errorf("want nil for safe commands, got %v", err)
	}
}

// TestCheckMutating_AptRemove_Blocked checks apt remove is blocked.
func TestCheckMutating_AptRemove_Blocked(t *testing.T) {
	err := CheckMutating("apt remove nginx")
	if err == nil {
		t.Fatal("want error for apt remove, got nil")
	}
}

// TestCheckMutating_YumInstall_Blocked checks yum install is blocked.
func TestCheckMutating_YumInstall_Blocked(t *testing.T) {
	err := CheckMutating("yum install httpd")
	if err == nil {
		t.Fatal("want error for yum install, got nil")
	}
}

// TestCheckMutating_DnfRemove_Blocked checks dnf remove is blocked.
func TestCheckMutating_DnfRemove_Blocked(t *testing.T) {
	err := CheckMutating("dnf remove nano")
	if err == nil {
		t.Fatal("want error for dnf remove, got nil")
	}
}

// TestCheckMutating_SystemctlStart_Blocked checks systemctl start is
// blocked.
func TestCheckMutating_SystemctlStart_Blocked(t *testing.T) {
	err := CheckMutating("systemctl start nginx")
	if err == nil {
		t.Fatal("want error for systemctl start, got nil")
	}
}

// TestCheckMutating_SystemctlStop_Blocked checks systemctl stop is
// blocked.
func TestCheckMutating_SystemctlStop_Blocked(t *testing.T) {
	err := CheckMutating("systemctl stop nginx")
	if err == nil {
		t.Fatal("want error for systemctl stop, got nil")
	}
}

// TestCheckMutating_SystemctlEnable_Blocked checks systemctl enable is
// blocked.
func TestCheckMutating_SystemctlEnable_Blocked(t *testing.T) {
	err := CheckMutating("systemctl enable nginx")
	if err == nil {
		t.Fatal("want error for systemctl enable, got nil")
	}
}

// TestCheckMutating_SystemctlDisable_Blocked checks systemctl disable.
func TestCheckMutating_SystemctlDisable_Blocked(t *testing.T) {
	err := CheckMutating("systemctl disable nginx")
	if err == nil {
		t.Fatal("want error for systemctl disable, got nil")
	}
}

// TestCheckMutating_SystemctlMask_Blocked checks systemctl mask.
func TestCheckMutating_SystemctlMask_Blocked(t *testing.T) {
	err := CheckMutating("systemctl mask nginx")
	if err == nil {
		t.Fatal("want error for systemctl mask, got nil")
	}
}

// TestCheckMutating_SystemctlReload_Blocked checks systemctl reload
// blocked.
func TestCheckMutating_SystemctlReload_Blocked(t *testing.T) {
	err := CheckMutating("systemctl reload nginx")
	if err == nil {
		t.Fatal("want error for systemctl reload, got nil")
	}
}

// TestCheckMutating_EmptyString_Allowed checks empty string is allowed.
func TestCheckMutating_EmptyString_Allowed(t *testing.T) {
	err := CheckMutating("")
	if err != nil {
		t.Errorf("want nil for empty string, got %v", err)
	}
}

// TestCheckMutating_PipeBypass_Blocked checks rm after pipe is blocked.
func TestCheckMutating_PipeBypass_Blocked(t *testing.T) {
	err := CheckMutating("ls | rm -f /tmp/x")
	if err == nil {
		t.Fatal("want error for rm after pipe, got nil")
	}
}

// TestCheckMutating_SubshellBypass_Blocked checks rm in subshell is
// blocked.
func TestCheckMutating_SubshellBypass_Blocked(t *testing.T) {
	err := CheckMutating("(rm -rf /tmp/x)")
	if err == nil {
		t.Fatal("want error for rm in subshell, got nil")
	}
}

// TestCheckMutating_DoublePipeBypass_Blocked checks rm after && |.
func TestCheckMutating_DoublePipeBypass_Blocked(t *testing.T) {
	err := CheckMutating("echo ok && ls | rm -f /tmp/x")
	if err == nil {
		t.Fatal("want error for rm after && |, got nil")
	}
}

// TestCheckMutating_PipeToEtc_Blocked checks redirect to /etc after
// pipe.
func TestCheckMutating_PipeToEtc_Blocked(t *testing.T) {
	err := CheckMutating("ls | grep foo > /etc/config")
	if err == nil {
		t.Fatal("want error for redirect to /etc after pipe, got nil")
	}
}

// TestCheckMutating_SubshellRedirect_Blocked checks redirect to /etc in
// subshell.
func TestCheckMutating_SubshellRedirect_Blocked(t *testing.T) {
	err := CheckMutating("(echo config > /etc/foo.conf)")
	if err == nil {
		t.Fatal("want error for redirect in subshell, got nil")
	}
}
