package tools

// Presets holds built-in permission rule presets keyed by name.
var Presets = map[string][]PermissionRule{
	"destructive": {
		{Match: "rm -rf *", Action: "deny", Reason: "recursive delete"},
		{Match: "rm -f *", Action: "ask", Reason: "force remove"},
		{Match: "mkfs *", Action: "deny", Reason: "format filesystem"},
		{Match: "dd if=*", Action: "deny", Reason: "raw disk write"},
		{Match: "truncate *", Action: "ask", Reason: "truncate file"},
		{Match: "shred *", Action: "deny", Reason: "secure delete"},
	},
	"service": {
		{Match: "systemctl start *", Action: "allow"},
		{Match: "systemctl stop *", Action: "ask", Reason: "service stop"},
		{Match: "systemctl restart *", Action: "ask", Reason: "service restart"},
		{Match: "systemctl enable *", Action: "allow"},
		{Match: "systemctl disable *", Action: "ask", Reason: "disable service"},
	},
	"package": {
		{Match: "apt install *", Action: "allow"},
		{Match: "apt remove *", Action: "ask", Reason: "package removal"},
		{Match: "yum install *", Action: "allow"},
		{Match: "yum remove *", Action: "ask", Reason: "package removal"},
	},
	"network": {
		{Match: "iptables *", Action: "ask", Reason: "firewall rule"},
		{Match: "ufw *", Action: "ask", Reason: "firewall change"},
		{Match: "netplan *", Action: "ask", Reason: "network config"},
	},
	"user": {
		{Match: "useradd *", Action: "ask", Reason: "user creation"},
		{Match: "userdel *", Action: "deny", Reason: "user deletion"},
		{Match: "usermod *", Action: "ask", Reason: "user modification"},
		{Match: "groupadd *", Action: "allow"},
		{Match: "groupdel *", Action: "deny", Reason: "group deletion"},
	},
	"ssh": {
		{Match: "ssh *", Action: "allow"},
		{Match: "scp *", Action: "allow"},
		{Match: "rsync *", Action: "allow"},
	},
	"aws": {
		{Match: "aws ec2 describe-*", Action: "allow"},
		{Match: "aws s3 ls", Action: "allow"},
		{Match: "aws ec2 terminate-instances *", Action: "deny", Reason: "instance termination"},
		{Match: "aws s3 rm *", Action: "ask", Reason: "S3 object deletion"},
	},
	"sudo": {
		{Match: "sudo systemctl *", Action: "ask", Reason: "sudo service management"},
		{Match: "sudo useradd *", Action: "ask", Reason: "sudo user creation"},
		{Match: "sudo userdel *", Action: "deny", Reason: "sudo user deletion"},
	},
}
