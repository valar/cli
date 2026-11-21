package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var settingNames = []string{api.SettingCPU, api.SettingMemory, api.SettingHTTP, api.SettingMinRunning}

const raiseHint = "Raising cpu, memory or minRunning needs a token with full access (scope all), and the project's budget caps what its services request; 'valar service settings' shows the budget."

// settingsRefusal adds a hint to a settings change the server refused for
// scope or budget. Its exit code stays that of the response.
func settingsRefusal(err error) error {
	var apiErr api.Error
	var jsonErr api.JSONError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden &&
		errors.As(apiErr.ServerError, &jsonErr) && jsonErr.Field != "" {
		return fmt.Errorf("%w\n%s", err, raiseHint)
	}
	return err
}

// applyDeclaredSettings sends the settings .valar.yml declares, so that the
// deployment a push creates pins them. It sends nothing if none are declared.
func applyDeclaredSettings(client *api.Client, cfg *config.ValidatedServiceConfig) error {
	declared := cfg.Unwrap().Deployment.Settings()
	if declared == nil {
		return nil
	}
	if _, err := client.PatchSettings(cfg.Project(), cfg.Service(), declared); err != nil {
		return settingsRefusal(fmt.Errorf("applying the settings in %s: %w", functionConfiguration, err))
	}
	return nil
}

var settingsService string

var serviceSettingsCmd = &cobra.Command{
	Use:   "settings [service]",
	Short: "Show the service's settings, which of them were set, and their limits.",
	Long: `Show the service's settings, which of them were set, and their limits.

cpu is in millicores and memory in MiB. Settings that were not set have the
platform's default. A change takes effect with the next deployment, which pins
the settings it runs with. Raising cpu, memory or minRunning needs a token with
full access, within the project's budget.`,
	Args: cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			settingsService = args[0]
		}
		client, cfg, err := settingsTarget()
		if err != nil {
			return err
		}
		view, err := client.ServiceSettings(cfg.Project(), cfg.Service())
		if err != nil {
			return err
		}
		return printSettings(stdout, view)
	}),
}

var serviceSettingsSetCmd = &cobra.Command{
	Use:   "set name=value...",
	Short: "Change settings, such as cpu=500 memory=2048.",
	Args:  cobra.MinimumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		patch, err := parseSettingAssignments(args)
		if err != nil {
			return err
		}
		return patchSettings(patch)
	}),
}

var serviceSettingsResetCmd = &cobra.Command{
	Use:   "reset name...",
	Short: "Reset settings to the platform's defaults.",
	Args:  cobra.MinimumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		patch := api.SettingsPatch{}
		for _, name := range args {
			if !slices.Contains(settingNames, name) {
				return unknownSetting(name)
			}
			patch[name] = nil
		}
		return patchSettings(patch)
	}),
}

func settingsTarget() (*api.Client, *config.ValidatedServiceConfig, error) {
	cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &settingsService, globalConfiguration)
	if err != nil {
		return nil, nil, err
	}
	client, err := globalConfiguration.APIClient()
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

func patchSettings(patch api.SettingsPatch) error {
	client, cfg, err := settingsTarget()
	if err != nil {
		return err
	}
	view, err := client.PatchSettings(cfg.Project(), cfg.Service(), patch)
	if err != nil {
		return settingsRefusal(err)
	}
	return printSettings(stdout, view)
}

func unknownSetting(name string) error {
	return newError(kindInvalid, "unknown setting %q, known settings are %s", name, strings.Join(settingNames, ", "))
}

// parseSettingAssignments reads name=value arguments: integers, and true or
// false for http.
func parseSettingAssignments(args []string) (api.SettingsPatch, error) {
	patch := api.SettingsPatch{}
	for _, arg := range args {
		name, value, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, newError(kindInvalid, "%q is not name=value", arg)
		}
		if !slices.Contains(settingNames, name) {
			return nil, unknownSetting(name)
		}
		if _, dup := patch[name]; dup {
			return nil, newError(kindInvalid, "%s is given twice", name)
		}
		if name == api.SettingHTTP {
			b, err := strconv.ParseBool(value)
			if err != nil {
				return nil, newError(kindInvalid, "http must be true or false")
			}
			patch[name] = b
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, newError(kindInvalid, "%s must be an integer", name)
		}
		patch[name] = n
	}
	return patch, nil
}

// printSettings prints the settings as a table, or the server's response with
// --json.
func printSettings(w io.Writer, view *api.SettingsView) error {
	return emit(w, view.Raw, func(w io.Writer) error {
		s, l := view.Settings, view.Limits
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SETTING\tVALUE\tSOURCE\tBOUNDS")
		for _, row := range []struct {
			name, value, bound string
		}{
			{api.SettingCPU, strconv.Itoa(s.CPU), boundText(l.CPU, "millicores")},
			{api.SettingMemory, strconv.Itoa(s.Memory), boundText(l.Memory, "MiB")},
			{api.SettingHTTP, strconv.FormatBool(s.HTTP), ""},
			{api.SettingMinRunning, strconv.Itoa(s.MinRunning), boundText(l.MinRunning, "")},
		} {
			source := "default"
			if slices.Contains(view.Explicit, row.name) {
				source = "set"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.name, row.value, source, row.bound)
		}
		tw.Flush()
		if b := l.Budget; b != nil {
			fmt.Fprintf(w, "Project budget: %d of %d MiB memory, %d of %d millicores CPU.\n",
				b.Memory.Used, b.Memory.Allowed, b.CPU.Used, b.CPU.Allowed)
		}
		if r := l.RunningInstances; r != nil {
			fmt.Fprintf(w, "Instances that always run: %d of %d.\n", r.Used, r.Allowed)
		}
		return nil
	})
}

func boundText(b api.SettingsBound, unit string) string {
	if b.Max == 0 {
		return ""
	}
	text := fmt.Sprintf("%d-%d", b.Min, b.Max)
	if unit != "" {
		text += " " + unit
	}
	if b.Step > 1 {
		text += fmt.Sprintf(", steps of %d", b.Step)
	}
	return text
}

func initSettingsCmd() {
	serviceSettingsCmd.PersistentFlags().StringVarP(&settingsService, "service", "s", "", "The service to target")
	serviceSettingsCmd.AddCommand(serviceSettingsSetCmd, serviceSettingsResetCmd)
	serviceCmd.AddCommand(serviceSettingsCmd)
}
