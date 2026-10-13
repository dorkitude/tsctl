package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var policyCmd = &cobra.Command{
	Use:     "policy",
	Aliases: []string{"acl"},
	Short:   "Read, validate, diff and replace the tailnet policy file",
}

var policyGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Print the policy file (HuJSON with comments; --json for plain JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			var v any
			if err := client.JSON("GET", "/tailnet/{tailnet}/acl", nil, &v); err != nil {
				return err
			}
			return printJSON(v)
		}
		p, err := client.GetPolicy()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(withNewline(p.HuJSON))
		return err
	},
}

var policyFile string

func readPolicyFile() ([]byte, error) {
	if policyFile == "" {
		return nil, fmt.Errorf("-f FILE is required (- for stdin)")
	}
	if policyFile == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(policyFile)
}

var policyValidateCmd = &cobra.Command{
	Use:   "validate -f FILE",
	Short: "Validate a policy file with Tailscale (runs its tests; saves nothing)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		b, err := readPolicyFile()
		if err != nil {
			return err
		}
		// Validation is a POST but writes nothing, so it's allowed in read-only mode.
		ro := client.ReadOnly
		client.ReadOnly = false
		err = client.ValidatePolicy(b)
		client.ReadOnly = ro
		if err != nil {
			return err
		}
		fmt.Println("policy is valid")
		return nil
	},
}

var policyDiffCmd = &cobra.Command{
	Use:   "diff -f FILE",
	Short: "Show the difference between the live policy and FILE (needs diff(1))",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		b, err := readPolicyFile()
		if err != nil {
			return err
		}
		p, err := client.GetPolicy()
		if err != nil {
			return err
		}
		return showDiff(p.HuJSON, b)
	},
}

var policyDryRun bool

var policySetCmd = &cobra.Command{
	Use:   "set -f FILE [--dry-run]",
	Short: "Replace the policy file (validates first; refuses if it changed meanwhile)",
	Long: `Replace the tailnet policy file with FILE (HuJSON or JSON).

tsctl fetches the live policy, shows the diff, validates FILE with Tailscale, and then
saves it with If-Match, so a concurrent edit in the admin console is never overwritten.
Back up first with: tsctl policy get > policy.backup.hujson`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		b, err := readPolicyFile()
		if err != nil {
			return err
		}
		cur, err := client.GetPolicy()
		if err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(cur.HuJSON), bytes.TrimSpace(b)) {
			fmt.Println("policy unchanged")
			return nil
		}
		if err := showDiff(cur.HuJSON, b); err != nil {
			return err
		}
		ro := client.ReadOnly
		client.ReadOnly = false
		err = client.ValidatePolicy(b)
		client.ReadOnly = ro
		if err != nil {
			return err
		}
		if policyDryRun {
			fmt.Println("policy is valid (dry run: not saved)")
			return nil
		}
		if err := client.SetPolicy(b, cur.ETag); err != nil {
			return err
		}
		fmt.Println("policy saved")
		return nil
	},
}

func showDiff(a, b []byte) error {
	if _, err := exec.LookPath("diff"); err != nil {
		fmt.Fprintln(os.Stderr, "(diff(1) not found; skipping diff)")
		return nil
	}
	dir, err := os.MkdirTemp("", "tsctl-diff-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	pa, pb := filepath.Join(dir, "live"), filepath.Join(dir, "new")
	if err := os.WriteFile(pa, withNewline(a), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(pb, withNewline(b), 0o600); err != nil {
		return err
	}
	c := exec.Command("diff", "-u", "--label", "live policy", "--label", policyFile, pa, pb)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil // differences found
		}
		return err
	}
	return nil
}

func withNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] != '\n' {
		return append(b, '\n')
	}
	return b
}

func init() {
	for _, c := range []*cobra.Command{policyValidateCmd, policyDiffCmd, policySetCmd} {
		c.Flags().StringVarP(&policyFile, "file", "f", "", "policy file (HuJSON or JSON; - for stdin)")
	}
	policySetCmd.Flags().BoolVar(&policyDryRun, "dry-run", false, "diff and validate, but don't save")
	policyCmd.AddCommand(policyGetCmd, policyValidateCmd, policyDiffCmd, policySetCmd)
	rootCmd.AddCommand(policyCmd)
}
