package cmd

import (
	"fmt"
	"os"
	"valar/cli/pkg/config"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
)

var domainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "Manage custom domains",
	Args:  cobra.ExactArgs(0),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		doms, err := client.ListDomains(cfg.Project)
		if err != nil {
			return err
		}
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
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		records, err := client.AddDomain(cfg.Project, args[0])
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

var domainsVerifyCmd = &cobra.Command{
	Use:   "verify [domain]",
	Short: "Verify a newly added domain",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		dom, err := client.VerifyDomain(cfg.Project, args[0])
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
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		svc := cfg.Service
		if len(args) == 2 {
			svc = args[1]
		}
		return client.LinkDomain(cfg.Project, args[0], svc)
	}),
}

func initDomainsCmd() {
	domainsCmd.AddCommand(domainsAddCmd)
	domainsCmd.AddCommand(domainsVerifyCmd)
	domainsCmd.AddCommand(domainsLinkCmd)
	rootCmd.AddCommand(domainsCmd)
}