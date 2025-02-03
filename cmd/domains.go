package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
	"golang.org/x/exp/slices"
)

var domainsCmd = &cobra.Command{
	Use:     "domain",
	Short:   "Manage custom domains.",
	Aliases: []string{"dom", "domains"},
}

var domainsListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all domains bound to the active project.",
	Aliases: []string{"l"},
	Args:    cobra.ExactArgs(0),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Attempt to read project from file if possible
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		doms, err := client.ListDomains(cfg.Project())
		if err != nil {
			return err
		}
		// Sort domains before printing them out
		slices.SortFunc(doms, func(a, b api.Domain) int { return strings.Compare(a.Domain, b.Domain) })
		// Print them pretty
		tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "DOMAIN\tVERIFIED\tSERVICE\tERROR")
		for _, d := range doms {
			svc := "<none>"
			if d.Service != nil {
				svc = *d.Service
			}
			fmt.Fprintf(tw, "%s\t%v\t%s\t%s\n", d.Domain, d.Verified, svc, d.Error)
		}
		tw.Flush()
		return nil
	}),
}

var domainsAddCmd = &cobra.Command{
	Use:   "add [domain]",
	Short: "Add a new domain to the project.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Attempt to read project from file if possible
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		records, err := client.AddDomain(cfg.Project(), args[0])
		if err != nil {
			return err
		}
		fmt.Println("Please set the following records (choose one for A/AAAA and CNAME):")
		fmt.Println()
		tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
		for rt, rr := range records {
			fmt.Fprintf(tw, "%s\t%s\n", rt, rr)
		}
		tw.Flush()
		return nil
	}),
}

var domainsDeleteCmd = &cobra.Command{
	Use:   "delete [domain]",
	Short: "Deletes an existing domain from the project.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Attempt to read project from file if possible
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		if err := client.DeleteDomain(cfg.Project(), args[0]); err != nil {
			return err
		}
		return nil
	}),
}

var domainsVerifyCmd = &cobra.Command{
	Use:   "verify [domain]",
	Short: "Verify a newly added domain.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Attempt to read project from file if possible
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		dom, err := client.VerifyDomain(cfg.Project(), args[0])
		if err != nil {
			return err
		}
		fmt.Println("Verified until", dom.Expiration)
		return nil
	}),
}

var domainsLinkAllowInsecureTraffic bool
var domainsLinkService string

var domainsLinkCmd = &cobra.Command{
	Use:   "link [--insecure] [--service service] domain",
	Short: "Link a domain to a service.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Attempt to read project from file if possible
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &domainsLinkService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return client.LinkDomain(api.LinkDomainArgs{
			Project:              cfg.Project(),
			Service:              cfg.Service(),
			Domain:               args[0],
			AllowInsecureTraffic: domainsLinkAllowInsecureTraffic,
		})
	}),
}

var domainsUnlinkService string

var domainsUnlinkCmd = &cobra.Command{
	Use:   "unlink [--service service] [domain]",
	Short: "Unlink a domain from a service.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Attempt to read project from file if possible
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &domainsUnlinkService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return client.UnlinkDomain(api.UnlinkDomainArgs{
			Project: cfg.Project(),
			Service: cfg.Service(),
			Domain:  args[0],
		})
	}),
}

func initDomainsCmd() {
	domainsLinkCmd.Flags().StringVarP(&domainsLinkService, "service", "s", "", "The service to link the domain to")
	domainsLinkCmd.Flags().BoolVarP(&domainsLinkAllowInsecureTraffic, "insecure", "i", false, "Allow insecure traffic to the service. Disables the default HTTPS redirect for this domain.")
	domainsUnlinkCmd.Flags().StringVarP(&domainsUnlinkService, "service", "s", "", "The service to unlink the domain from")
	domainsCmd.AddCommand(domainsListCmd, domainsAddCmd, domainsVerifyCmd, domainsLinkCmd, domainsUnlinkCmd, domainsDeleteCmd)
	rootCmd.AddCommand(domainsCmd)
}
