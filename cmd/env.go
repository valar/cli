package cmd

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
	"golang.org/x/term"
)

var (
	envService   string
	envFormat    string
	envBuild     bool
	envAll       bool
	envSetSecret bool
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage the environment variables of a service.",
	Long: `Manage the environment variables of a service.

Variables are stored by the platform, per service and target: runtime variables
reach deployments and batch executions, build variables reach builds. Commands
act on runtime variables unless --build or --all is given. Secret values are
write-only: nothing shows them once they are set.`,
}

var envListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List the variables of the service.",
	Aliases: []string{"ls"},
	Args:    cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, client, err := envContext()
		if err != nil {
			return err
		}
		vars, err := client.ListEnv(cfg.Project(), cfg.Service())
		if err != nil {
			return err
		}
		return printEnv(os.Stdout, filterEnv(vars, envTargets(envBuild, envAll)), envFormat)
	}),
}

var envSetCmd = &cobra.Command{
	Use:   "set NAME=VALUE... | NAME",
	Short: "Set variables.",
	Long: `Set variables.

Given only a NAME, the value is read from standard input, or prompted for
without echo on a terminal. Prefer that for secrets: arguments end up in shell
history and process listings.`,
	Args: cobra.MinimumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, client, err := envContext()
		if err != nil {
			return err
		}
		values, err := parseEnvAssignments(args, func(name string) (string, error) {
			return readEnvValue(os.Stdin, name)
		})
		if err != nil {
			return err
		}
		targets := envTargets(envBuild, envAll)
		changes := &api.EnvChangeset{Set: map[string]api.EnvValue{}}
		for name, value := range values {
			changes.Set[name] = api.EnvValue{Value: value, Secret: envSetSecret, Targets: targets}
		}
		if _, err := client.PatchEnv(cfg.Project(), cfg.Service(), changes); err != nil {
			return err
		}
		fmt.Printf("Set %s (%s).\n", strings.Join(slices.Sorted(maps.Keys(values)), ", "), strings.Join(targets, ", "))
		return nil
	}),
}

var envUnsetCmd = &cobra.Command{
	Use:     "unset NAME...",
	Short:   "Remove variables.",
	Aliases: []string{"delete", "rm"},
	Args:    cobra.MinimumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, client, err := envContext()
		if err != nil {
			return err
		}
		targets := envTargets(envBuild, envAll)
		changes := &api.EnvChangeset{Unset: map[string][]string{}}
		for _, name := range args {
			changes.Unset[name] = targets
		}
		if _, err := client.PatchEnv(cfg.Project(), cfg.Service(), changes); err != nil {
			return err
		}
		fmt.Printf("Unset %s (%s).\n", strings.Join(args, ", "), strings.Join(targets, ", "))
		return nil
	}),
}

var envHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show the change log of the variables, newest first.",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, client, err := envContext()
		if err != nil {
			return err
		}
		changes, err := client.ListEnvChanges(cfg.Project(), cfg.Service())
		if err != nil {
			return err
		}
		targets := envTargets(envBuild, envAll)
		tw := tabwriter.NewWriter(os.Stdout, 0, 1, 1, ' ', 0)
		fmt.Fprintln(tw, "TIME\tACTION\tNAME\tTARGET\tSECRET\tORIGIN\tBY")
		for _, c := range changes {
			if !slices.Contains(targets, c.Target) {
				continue
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%s\t%s\n",
				c.CreatedAt.Local().Format("2006-01-02 15:04:05"), c.Action, c.Name, c.Target, c.Secret, c.Origin, c.CreatedBy)
		}
		return tw.Flush()
	}),
}

var envImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Move the environment lists of .valar.yml to the service.",
	Long: `Move the environment lists of .valar.yml to the service.

Plain entries become plain variables wherever the service still holds the value
the platform seeded from the last push, so a variable changed since is left
alone. Secret entries cannot be moved, because the file holds only their
ciphertexts: the command names those the service lacks, to be set again with
'valar env set --secret'. Then it empties the lists in .valar.yml.`,
	Args: cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		if envService != "" {
			return fmt.Errorf("import uses the service named in %s, drop --service", functionConfiguration)
		}
		cfg, err := config.NewServiceConfigFromFile(functionConfiguration)
		if err != nil {
			return fmt.Errorf("read %s: %w", functionConfiguration, err)
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return importEnv(client, cfg, os.Stdout)
	}),
}

func envContext() (config.ServiceConfig, *api.Client, error) {
	cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &envService, globalConfiguration)
	if err != nil {
		return nil, nil, err
	}
	client, err := globalConfiguration.APIClient()
	if err != nil {
		return nil, nil, err
	}
	return cfg, client, nil
}

func envTargets(build, all bool) []string {
	switch {
	case all:
		return []string{api.EnvTargetRuntime, api.EnvTargetBuild}
	case build:
		return []string{api.EnvTargetBuild}
	default:
		return []string{api.EnvTargetRuntime}
	}
}

// filterEnv keeps the variables of the given targets, runtime first, then by name.
func filterEnv(vars []api.EnvVariable, targets []string) []api.EnvVariable {
	var kept []api.EnvVariable
	for _, v := range vars {
		if slices.Contains(targets, v.Target) {
			kept = append(kept, v)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Target != kept[j].Target {
			return kept[i].Target == api.EnvTargetRuntime
		}
		return kept[i].Name < kept[j].Name
	})
	return kept
}

func printEnv(w io.Writer, vars []api.EnvVariable, format string) error {
	switch format {
	case "raw":
		// A secret has no value to print, and NAME= would claim an empty one.
		for _, v := range vars {
			if !v.Secret {
				fmt.Fprintf(w, "%s=%s\n", v.Name, v.Value)
			}
		}
		return nil
	case "table":
		tw := tabwriter.NewWriter(w, 0, 1, 1, ' ', 0)
		fmt.Fprintln(tw, "NAME\tTARGET\tSECRET\tVALUE\tUPDATED\tBY")
		for _, v := range vars {
			value := v.Value
			if v.Secret {
				value = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%v\t%s\t%s\t%s\n", v.Name, v.Target, v.Secret, value, humanize.Time(v.UpdatedAt), v.UpdatedBy)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("unknown env format %s", format)
	}
}

// parseEnvAssignments parses NAME=VALUE arguments. A bare NAME takes its value
// from read, which may happen once since it consumes standard input.
func parseEnvAssignments(args []string, read func(name string) (string, error)) (map[string]string, error) {
	values := make(map[string]string, len(args))
	prompted := false
	for _, arg := range args {
		name, value, ok := strings.Cut(arg, "=")
		if name == "" {
			return nil, fmt.Errorf("bad argument %q, must be NAME=VALUE or NAME", arg)
		}
		if _, dup := values[name]; dup {
			return nil, fmt.Errorf("variable %s is given twice", name)
		}
		if !ok {
			if prompted {
				return nil, fmt.Errorf("only one variable can take its value from standard input")
			}
			prompted = true
			var err error
			if value, err = read(name); err != nil {
				return nil, fmt.Errorf("read value of %s: %w", name, err)
			}
		}
		values[name] = value
	}
	return values, nil
}

// readEnvValue prompts for a value without echo on a terminal, and otherwise
// reads all of stdin without its final line break.
func readEnvValue(stdin *os.File, name string) (string, error) {
	if term.IsTerminal(int(stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "Value of %s: ", name)
		value, err := term.ReadPassword(int(stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(value), err
	}
	return readValue(stdin)
}

func readValue(r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if value == "" {
		return "", errors.New("standard input is empty, pass NAME= to set an empty value")
	}
	return value, nil
}

// importEnv moves the inline lists of cfg to the service and empties them in
// the file.
func importEnv(client *api.Client, cfg *config.ValidatedServiceConfig, w io.Writer) error {
	yamlCfg := cfg.Unwrap()
	lists := map[string][]config.EnvironmentConfig{}
	if yamlCfg.Deployment != nil {
		lists[api.EnvTargetRuntime] = yamlCfg.Deployment.Environment
	}
	if yamlCfg.Build != nil {
		lists[api.EnvTargetBuild] = yamlCfg.Build.Environment
	}
	if len(lists[api.EnvTargetRuntime]) == 0 && len(lists[api.EnvTargetBuild]) == 0 {
		fmt.Fprintf(w, "The environment lists in %s are empty, nothing to import.\n", functionConfiguration)
		return nil
	}
	// Reading first also stops before any change on a server without the API.
	stored, err := client.ListEnv(cfg.Project(), cfg.Service())
	if err != nil {
		return err
	}
	for _, target := range []string{api.EnvTargetRuntime, api.EnvTargetBuild} {
		plain, secrets := splitInlineList(lists[target])
		if len(plain) > 0 {
			changes := &api.EnvChangeset{Set: map[string]api.EnvValue{}, OnlySeeded: true}
			for name, value := range plain {
				changes.Set[name] = api.EnvValue{Value: value, Targets: []string{target}}
			}
			result, err := client.PatchEnv(cfg.Project(), cfg.Service(), changes)
			if err != nil {
				return fmt.Errorf("import %s variables: %w", target, err)
			}
			var imported []string
			for _, name := range slices.Sorted(maps.Keys(plain)) {
				if !slices.Contains(result.Skipped, name) {
					imported = append(imported, name)
				}
			}
			if len(imported) > 0 {
				fmt.Fprintf(w, "Imported the plain %s variables %s.\n", target, strings.Join(imported, ", "))
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "Left %s as stored, since they changed on the service after the seed.\n", strings.Join(result.Skipped, ", "))
			}
		}
		if missing := missingSecrets(secrets, stored, target); len(missing) > 0 {
			flag := ""
			if target == api.EnvTargetBuild {
				flag = " --build"
			}
			fmt.Fprintf(w, "The service lacks the %s secrets %s. Set each again with 'valar env set --secret%s NAME'.\n",
				target, strings.Join(missing, ", "), flag)
		}
	}
	if yamlCfg.Deployment != nil {
		yamlCfg.Deployment.Environment = []config.EnvironmentConfig{}
	}
	if yamlCfg.Build != nil {
		yamlCfg.Build.Environment = []config.EnvironmentConfig{}
	}
	if err := yamlCfg.WriteBack(); err != nil {
		return err
	}
	fmt.Fprintf(w, "Emptied the environment lists in %s.\n", functionConfiguration)
	return nil
}

// splitInlineList resolves an inline list the way the server applies it: the
// last entry of a repeated name wins.
func splitInlineList(list []config.EnvironmentConfig) (plain map[string]string, secrets []string) {
	plain = map[string]string{}
	secret := map[string]bool{}
	for _, kv := range list {
		if kv.Secret {
			delete(plain, kv.Key)
			secret[kv.Key] = true
		} else {
			delete(secret, kv.Key)
			plain[kv.Key] = kv.Value
		}
	}
	return plain, slices.Sorted(maps.Keys(secret))
}

func missingSecrets(names []string, stored []api.EnvVariable, target string) []string {
	have := map[string]bool{}
	for _, v := range stored {
		if v.Target == target {
			have[v.Name] = true
		}
	}
	var missing []string
	for _, name := range names {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// warnInlineEnvironment points out lists in .valar.yml that requests no longer carry.
func warnInlineEnvironment(cfg config.ServiceConfigYAML) {
	n := 0
	if cfg.Build != nil {
		n += len(cfg.Build.Environment)
	}
	if cfg.Deployment != nil {
		n += len(cfg.Deployment.Environment)
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "Warning: the environment lists in %s are ignored, the service's stored variables apply. Run 'valar env import' to move them.\n", functionConfiguration)
	}
}

func initEnvCmd() {
	envCmd.PersistentFlags().StringVarP(&envService, "service", "s", "", "The service to manage")
	envCmd.PersistentFlags().BoolVarP(&envBuild, "build", "b", false, "Build variables instead of runtime variables")
	envCmd.PersistentFlags().BoolVarP(&envAll, "all", "a", false, "Both runtime and build variables")
	envSetCmd.Flags().BoolVar(&envSetSecret, "secret", false, "Store the value write-only, so it is never shown again")
	envListCmd.Flags().StringVar(&envFormat, "format", "table", "Display format (table|raw); raw prints NAME=VALUE for plain variables only")
	envCmd.AddCommand(envListCmd, envSetCmd, envUnsetCmd, envHistoryCmd, envImportCmd)
	rootCmd.AddCommand(envCmd)
}
