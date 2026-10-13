package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dorkitude/tsctl/internal/tsapi"
	"github.com/spf13/cobra"
)

var keysCmd = &cobra.Command{
	Use:     "keys",
	Aliases: []string{"key"},
	Short:   "List, create and revoke auth keys",
}

var keysListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List keys (secrets are never shown)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		keys, err := client.Keys()
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(keys)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTYPE\tDESCRIPTION\tTAGS\tREUSABLE\tEXPIRES")
		for _, k := range keys {
			c := k.Capabilities.Devices.Create
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", k.ID, k.KeyType, k.Description,
				strings.Join(c.Tags, ","), yesNo(c.Reusable), k.Expires)
		}
		return w.Flush()
	},
}

var (
	keyOpts   tsapi.AuthKeyOptions
	keyExpiry time.Duration
	keyOut    string
)

var keysCreateCmd = &cobra.Command{
	Use:   "create [--tag tag:x] [--reusable] [--ephemeral] [--preauthorized] [--key-out FILE]",
	Short: "Create a device auth key",
	Long: `Create a device auth key. The key is shown once. With --key-out it is written to
FILE (mode 0600) instead of stdout, so it never lands in a terminal or log.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		for i, t := range keyOpts.Tags {
			keyOpts.Tags[i] = normTag(t)
		}
		keyOpts.ExpirySeconds = int(keyExpiry.Seconds())
		k, err := client.CreateAuthKey(keyOpts)
		if err != nil {
			return err
		}
		if keyOut != "" {
			if err := os.WriteFile(keyOut, []byte(k.Key+"\n"), 0o600); err != nil {
				return err
			}
			k.Key = ""
			if jsonOutput {
				return printJSON(k)
			}
			fmt.Printf("created key %s (expires %s); secret written to %s\n", k.ID, k.Expires, keyOut)
			return nil
		}
		if jsonOutput {
			return printJSON(k)
		}
		fmt.Printf("created key %s (expires %s)\n%s\n", k.ID, k.Expires, k.Key)
		return nil
	},
}

var keysDeleteCmd = &cobra.Command{
	Use:     "delete KEY_ID",
	Aliases: []string{"revoke", "rm"},
	Short:   "Revoke a key",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := client.DeleteKey(args[0]); err != nil {
			return err
		}
		fmt.Printf("revoked %s\n", args[0])
		return nil
	},
}

func init() {
	f := keysCreateCmd.Flags()
	f.StringSliceVar(&keyOpts.Tags, "tag", nil, "tag the devices that use this key (repeatable)")
	f.BoolVar(&keyOpts.Reusable, "reusable", false, "allow more than one device to use it")
	f.BoolVar(&keyOpts.Ephemeral, "ephemeral", false, "devices are removed when they go offline")
	f.BoolVar(&keyOpts.Preauthorized, "preauthorized", false, "skip device approval")
	f.DurationVar(&keyExpiry, "expiry", 0, "key lifetime, e.g. 1h (default: Tailscale's 90 days)")
	f.StringVar(&keyOpts.Description, "description", "", "description")
	f.StringVar(&keyOut, "key-out", "", "write the key to FILE (0600) instead of printing it")
	keysCmd.AddCommand(keysListCmd, keysCreateCmd, keysDeleteCmd)
	rootCmd.AddCommand(keysCmd)
}
