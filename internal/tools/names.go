package tools

// Tool name constants eliminate scattered string literals and make
// refactoring safer. Keep in sync with each tool's Name() return value.
const (
	ToolShell     = "shell"
	ToolSSH       = "ssh"
	ToolSudo      = "sudo"
	ToolAWS       = "aws"
	ToolFileRead  = "file_read"
	ToolFileEdit  = "file_edit"
	ToolFileWrite = "file_write"
	ToolGrep      = "grep"
	ToolFind      = "find"
	ToolWebSearch = "websearch"
	ToolWebFetch  = "webfetch"
	ToolLedgerGet = "ledger_get"
	ToolLedgerSet = "ledger_set"
	ToolTask      = "task"
)
