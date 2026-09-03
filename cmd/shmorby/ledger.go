package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"shmorby/internal/ledger"
	"shmorby/internal/redact"
)

var ledgerCmd = &cobra.Command{
	Use:   "ledger",
	Short: "Manage the encrypted environment ledger",
}

var ledgerGetCmd = &cobra.Command{
	Use:   "get <section>",
	Short: "Print a ledger section as JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runLedgerGet,
}

var ledgerSetCmd = &cobra.Command{
	Use:   "set <section> <json>",
	Short: "Replace a ledger section with JSON",
	Args:  cobra.ExactArgs(2),
	RunE:  runLedgerSet,
}

var ledgerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all ledger section names",
	Args:  cobra.NoArgs,
	RunE:  runLedgerList,
}

var ledgerDeleteCmd = &cobra.Command{
	Use:   "delete <section>",
	Short: "Remove a ledger section",
	Args:  cobra.ExactArgs(1),
	RunE:  runLedgerDelete,
}

func init() {
	ledgerCmd.AddCommand(ledgerGetCmd)
	ledgerCmd.AddCommand(ledgerSetCmd)
	ledgerCmd.AddCommand(ledgerListCmd)
	ledgerCmd.AddCommand(ledgerDeleteCmd)
}

func runLedgerGet(cmd *cobra.Command, args []string) error {
	section := args[0]

	if err := ledger.ValidateSection(section); err != nil {
		return err
	}

	l, err := ledger.Open()
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer l.Close()

	data, ok := l.Get(section)
	if !ok {
		return fmt.Errorf("section %q not found", section)
	}

	// Pretty-print for human readability.
	var pretty json.RawMessage = data
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return fmt.Errorf("format JSON: %w", err)
	}

	fmt.Fprintln(os.Stdout, string(out))

	return nil
}

func runLedgerSet(cmd *cobra.Command, args []string) error {
	section := args[0]
	raw := args[1]

	if err := ledger.ValidateSection(section); err != nil {
		return err
	}

	// Validate that the input is valid JSON before storing.
	if !json.Valid([]byte(raw)) {
		return fmt.Errorf("invalid JSON: %s", raw)
	}

	// Redact secrets before storage — same guarantee as the
	// ledger_set agent tool: walk the decoded JSON tree so
	// JSON-keyed secrets are caught and output stays valid.
	redacted, err := redact.JSONData([]byte(raw))
	if err != nil {
		return fmt.Errorf("redact data: %w", err)
	}

	l, err := ledger.Open()
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}

	// Enforce size caps before storing, mirroring the agent tool.
	// Replacing an existing section does not increase the count.
	existingCount := len(l.Sections())
	_, exists := l.Get(section)
	countForCheck := existingCount
	if exists {
		countForCheck = existingCount - 1
	}

	if err := ledger.ValidateData(
		json.RawMessage(redacted), countForCheck,
	); err != nil {
		_ = l.Close()

		return fmt.Errorf("ledger cap exceeded: %w", err)
	}

	l.Set(section, json.RawMessage(redacted))

	if err := l.Close(); err != nil {
		return fmt.Errorf("close ledger: %w", err)
	}

	return nil
}

func runLedgerList(cmd *cobra.Command, args []string) error {
	l, err := ledger.Open()
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer l.Close()

	sections := l.Sections()
	if len(sections) == 0 {
		fmt.Fprintln(os.Stdout, "Ledger is empty.")

		return nil
	}

	for _, s := range sections {
		fmt.Fprintln(os.Stdout, s)
	}

	return nil
}

func runLedgerDelete(cmd *cobra.Command, args []string) error {
	section := args[0]

	if err := ledger.ValidateSection(section); err != nil {
		return err
	}

	l, err := ledger.Open()
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}

	if _, ok := l.Get(section); !ok {
		_ = l.Close()

		return fmt.Errorf("section %q not found", section)
	}

	l.Delete(section)

	if err := l.Close(); err != nil {
		return fmt.Errorf("close ledger: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(ledgerCmd)
}
