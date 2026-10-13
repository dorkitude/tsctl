package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dorkitude/tsctl/internal/tsapi"
	"github.com/spf13/cobra"
)

var apiData string

var apiCmd = &cobra.Command{
	Use:   "api METHOD PATH [-d BODY|@FILE|@-]",
	Short: "Call any Tailscale API endpoint",
	Long: `Call any endpoint of https://api.tailscale.com/api/v2. "{tailnet}" in PATH becomes
the tailnet (default "-"). Writes are refused under --read-only.

  tsctl api GET /tailnet/{tailnet}/settings
  tsctl api PATCH /tailnet/{tailnet}/settings -d '{"devicesApprovalOn":true}'`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var body []byte
		switch {
		case apiData == "@-":
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			body = b
		case strings.HasPrefix(apiData, "@"):
			b, err := os.ReadFile(apiData[1:])
			if err != nil {
				return err
			}
			body = b
		case apiData != "":
			body = []byte(apiData)
		}
		resp, err := client.Do(tsapi.Request{Method: args[0], Path: args[1], Body: body})
		if resp != nil && len(resp.Body) > 0 {
			var pretty bytes.Buffer
			if json.Indent(&pretty, resp.Body, "", "  ") == nil {
				fmt.Println(pretty.String())
			} else {
				os.Stdout.Write(withNewline(resp.Body))
			}
		}
		return err
	},
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Credential status",
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which credential tsctl uses and check it works (never prints secrets)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := client.AuthSource()
		if err != nil {
			return err
		}
		fmt.Println("credential:", src)
		devs, err := client.Devices()
		if err != nil {
			fmt.Println("devices:read: FAILED:", err)
		} else {
			fmt.Printf("devices:read: ok (%d devices)\n", len(devs))
		}
		if _, err := client.GetPolicy(); err != nil {
			fmt.Println("policy_file:read: FAILED:", err)
		} else {
			fmt.Println("policy_file:read: ok")
		}
		return nil
	},
}

func init() {
	apiCmd.Flags().StringVarP(&apiData, "data", "d", "", "request body, @FILE, or @- for stdin")
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(apiCmd, authCmd)
}
