package cmd

import (
	"fmt"
	"os"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
)

var domainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "Manage custom domains",
	Args:  cobra.ExactArgs(0),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		var project string
		// Attempt to read project from file if possible
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			project = globalConfiguration.Project()
		} else {
			project = cfg.Project
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		doms, err := client.ListDomains(project)
		if err != nil {
			return err
		}
		// Sort domains before printing them out
		slices.SortFunc(doms, func(a, b api.Domain) bool { return a.Domain < b.Domain })
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
	Short: "Add a new domain to the project",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		var project string
		// Attempt to read project from file if possible
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			// Fall back to global project
			project = globalConfiguration.Project()
		} else {
			project = cfg.Project
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		records, err := client.AddDomain(project, args[0])
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

var domainsRemoveCmd = &cobra.Command{
	Use:   "remove [domain]",
	Short: "Removes an existing domain from the project",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		var project string
		// Attempt to read project from file if possible
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			// Fall back to global project
			project = globalConfiguration.Project()
		} else {
			project = cfg.Project
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		if err := client.RemoveDomain(project, args[0]); err != nil {
			return err
		}
		return nil
	}),
}

var domainsVerifyCmd = &cobra.Command{
	Use:   "verify [domain]",
	Short: "Verify a newly added domain",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		var project string
		// Attempt to read project from file if possible
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			// Fall back to global project
			project = globalConfiguration.Project()
		} else {
			project = cfg.Project
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		dom, err := client.VerifyDomain(project, args[0])
		if err != nil {
			return err
		}
		fmt.Println("Verified until", dom.Expiration)
		return nil
	}),
}

var domainsLinkCmd = &cobra.Command{
	Use:   "link [domain] ([service])",
	Short: "Link a domain to a service",
	Args:  cobra.RangeArgs(1, 2),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		var project, svc string
		// Attempt to read project from file if possible
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			project = globalConfiguration.Project()
		} else {
			project = cfg.Project
			svc = cfg.Service
		}
		// If arg exists, replace service
		if len(args) == 2 {
			svc = args[1]
		}
		// If svc is still missing, fail
		if svc == "" {
			return fmt.Errorf("missing service name")
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return client.LinkDomain(project, args[0], svc)
	}),
}

var domainsUnlinkCmd = &cobra.Command{
	Use:   "unlink [domain] ([service])",
	Short: "Unlink a domain from a service",
	Args:  cobra.RangeArgs(1, 2),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		var project, svc string
		// Attempt to read project from file if possible
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			project = globalConfiguration.Project()
		} else {
			project = cfg.Project
			svc = cfg.Service
		}
		// If arg exists, replace service
		if len(args) == 2 {
			svc = args[1]
		}
		// If svc is still missing, fail
		if svc == "" {
			return fmt.Errorf("missing service name")
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return client.UnlinkDomain(project, args[0], svc)
	}),
}

func initDomainsCmd() {
	domainsCmd.AddCommand(domainsAddCmd)
	domainsCmd.AddCommand(domainsVerifyCmd)
	domainsCmd.AddCommand(domainsLinkCmd)
	domainsCmd.AddCommand(domainsUnlinkCmd)
	domainsCmd.AddCommand(domainsRemoveCmd)
	rootCmd.AddCommand(domainsCmd)
}
