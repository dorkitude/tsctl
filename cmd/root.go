// Package cmd holds tsctl's Cobra commands.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dorkitude/tsctl/internal/tsapi"
	"github.com/spf13/cobra"
)

var (
	jsonOutput   bool
	readOnlyFlag bool
	debugFlag    bool
	tailnetFlag  string
	client       *tsapi.Client
)

var rootCmd = &cobra.Command{
	Use:   "tsctl",
	Short: "Administer a Tailscale tailnet: devices, tags, policy file, auth keys, raw API",
	Long: `tsctl administers a Tailscale tailnet from the terminal, through the Tailscale API.
(The tailscale CLI controls the local node; tsctl does what the admin console does.)

Areas:
  auth status                           which credential is in use (never prints it)
  devices list|get|tag|expiry|authorize|delete
  policy get|validate|diff|set          the policy file (HuJSON, comments kept)
  keys list|create|delete               auth keys
  api METHOD PATH                       any other endpoint

Credentials (first match wins, never printed):
  TS_API_KEY                            API access token (tskey-api-...)
  TS_OAUTH_CLIENT_ID + _SECRET          OAuth client
  exe.dev Tailscale WIF integration     TSCTL_WIF_URL (default ` + tsapi.DefaultWIFURL + `)

Safety: --read-only (or TSCTL_READONLY=1) refuses every write before it's sent.
Agents: use --json on reads.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		client = tsapi.New()
		if tailnetFlag != "" {
			client.Tailnet = tailnetFlag
		}
		ro := os.Getenv("TSCTL_READONLY")
		client.ReadOnly = readOnlyFlag || (ro != "" && ro != "0")
		if debugFlag {
			client.Debug = os.Stderr
		}
	},
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.BoolVar(&jsonOutput, "json", false, "output JSON")
	pf.BoolVar(&readOnlyFlag, "read-only", false, "refuse every write (also TSCTL_READONLY=1)")
	pf.BoolVar(&debugFlag, "debug", false, "log each request's method, path and status to stderr")
	pf.StringVar(&tailnetFlag, "tailnet", "", `tailnet ID (default "-", the credential's own tailnet; also TSCTL_TAILNET)`)
}

// SetVersion sets the version shown by --version.
func SetVersion(v string) { rootCmd.Version = v }

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tsctl:", err)
		os.Exit(1)
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
