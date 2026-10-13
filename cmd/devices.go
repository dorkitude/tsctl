package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/dorkitude/tsctl/internal/tsapi"
	"github.com/spf13/cobra"
)

var devicesCmd = &cobra.Command{
	Use:     "devices",
	Aliases: []string{"device", "dev"},
	Short:   "List and manage devices (DEVICE = name, hostname, IP, or ID)",
}

var devicesListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List devices",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		devs, err := client.Devices()
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(devs)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tIP\tOS\tOWNER/TAGS\tONLINE\tKEY EXPIRY\tID")
		for _, d := range devs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.ShortName(), firstIPv4(d), d.OS, owner(d),
				yesNo(d.ConnectedToControl), expiry(d), d.ID)
		}
		return w.Flush()
	},
}

var devicesGetCmd = &cobra.Command{
	Use:   "get DEVICE",
	Short: "Show one device",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := client.FindDevice(args[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(d)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, row := range [][2]string{
			{"Name", d.Name}, {"Hostname", d.Hostname}, {"ID", d.ID}, {"Node ID", d.NodeID},
			{"Addresses", strings.Join(d.Addresses, ", ")}, {"OS", d.OS}, {"User", d.User},
			{"Tags", strings.Join(d.Tags, ", ")}, {"Authorized", yesNo(d.Authorized)},
			{"Online", yesNo(d.ConnectedToControl)}, {"Last seen", d.LastSeen}, {"Key expiry", expiry(d)},
		} {
			fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
		}
		return w.Flush()
	},
}

var (
	tagAdd, tagRemove []string
)

var devicesTagCmd = &cobra.Command{
	Use:   "tag DEVICE [tag:x ...]",
	Short: "Set a device's tags (replaces them), or --add / --remove some",
	Long: `Set a device's ACL tags. With tags as arguments, they replace the current set.
With --add/--remove, the current set is edited. Tagging a user-owned device makes it
a tagged device (it stops belonging to the user). Tags must exist in tagOwners.

  tsctl devices tag muse tag:muse
  tsctl devices tag muse --add tag:agent
  tsctl devices tag muse --remove tag:old`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 && (len(tagAdd) > 0 || len(tagRemove) > 0) {
			return fmt.Errorf("give tags as arguments or with --add/--remove, not both")
		}
		if len(args) == 1 && len(tagAdd) == 0 && len(tagRemove) == 0 {
			return fmt.Errorf("no tags given")
		}
		d, err := client.FindDevice(args[0])
		if err != nil {
			return err
		}
		tags := args[1:]
		if len(args) == 1 {
			tags = slices.Clone(d.Tags)
			for _, t := range tagAdd {
				if !slices.Contains(tags, normTag(t)) {
					tags = append(tags, normTag(t))
				}
			}
			tags = slices.DeleteFunc(tags, func(t string) bool {
				return slices.ContainsFunc(tagRemove, func(r string) bool { return normTag(r) == t })
			})
		}
		for i := range tags {
			tags[i] = normTag(tags[i])
		}
		if err := client.SetTags(d.ID, tags); err != nil {
			return err
		}
		fmt.Printf("%s tags: %s\n", d.ShortName(), orNone(strings.Join(tags, ", ")))
		return nil
	},
}

var expiryDisable, expiryEnable bool

var devicesExpiryCmd = &cobra.Command{
	Use:   "expiry DEVICE --disable|--enable",
	Short: "Turn node key expiry off or on",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if expiryDisable == expiryEnable {
			return fmt.Errorf("pass exactly one of --disable or --enable")
		}
		d, err := client.FindDevice(args[0])
		if err != nil {
			return err
		}
		if err := client.SetKeyExpiryDisabled(d.ID, expiryDisable); err != nil {
			return err
		}
		fmt.Printf("%s key expiry: %s\n", d.ShortName(), map[bool]string{true: "disabled", false: "enabled"}[expiryDisable])
		return nil
	},
}

var deauthorize bool

var devicesAuthorizeCmd = &cobra.Command{
	Use:   "authorize DEVICE [--deauthorize]",
	Short: "Authorize (or deauthorize) a device",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := client.FindDevice(args[0])
		if err != nil {
			return err
		}
		if err := client.SetAuthorized(d.ID, !deauthorize); err != nil {
			return err
		}
		fmt.Printf("%s authorized: %s\n", d.ShortName(), yesNo(!deauthorize))
		return nil
	},
}

var deleteYes bool

var devicesDeleteCmd = &cobra.Command{
	Use:   "delete DEVICE --yes",
	Short: "Remove a device from the tailnet",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := client.FindDevice(args[0])
		if err != nil {
			return err
		}
		if !deleteYes {
			return fmt.Errorf("this removes %s (%s) from the tailnet; re-run with --yes", d.Name, d.ID)
		}
		if err := client.DeleteDevice(d.ID); err != nil {
			return err
		}
		fmt.Printf("deleted %s\n", d.Name)
		return nil
	},
}

func init() {
	devicesTagCmd.Flags().StringSliceVar(&tagAdd, "add", nil, "tags to add")
	devicesTagCmd.Flags().StringSliceVar(&tagRemove, "remove", nil, "tags to remove")
	devicesExpiryCmd.Flags().BoolVar(&expiryDisable, "disable", false, "disable key expiry")
	devicesExpiryCmd.Flags().BoolVar(&expiryEnable, "enable", false, "enable key expiry")
	devicesAuthorizeCmd.Flags().BoolVar(&deauthorize, "deauthorize", false, "deauthorize instead")
	devicesDeleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "confirm deletion")
	devicesCmd.AddCommand(devicesListCmd, devicesGetCmd, devicesTagCmd, devicesExpiryCmd, devicesAuthorizeCmd, devicesDeleteCmd)
	rootCmd.AddCommand(devicesCmd)
}

func normTag(t string) string {
	if strings.HasPrefix(t, "tag:") {
		return t
	}
	return "tag:" + t
}

func firstIPv4(d tsapi.Device) string {
	for _, a := range d.Addresses {
		if !strings.Contains(a, ":") {
			return a
		}
	}
	if len(d.Addresses) > 0 {
		return d.Addresses[0]
	}
	return ""
}

func owner(d tsapi.Device) string {
	if len(d.Tags) > 0 {
		return strings.Join(d.Tags, ",")
	}
	return d.User
}

func expiry(d tsapi.Device) string {
	if d.KeyExpiryDisabled {
		return "disabled"
	}
	return d.Expires
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
