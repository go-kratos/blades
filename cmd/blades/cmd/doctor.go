package cmd

import (
	"errors"
	"fmt"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check workspace health",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loadConfigForCommand(cmd); err != nil {
				return err
			}
			ctx := appcore.BuildDoctorContext(commandOptions(cmd))
			results, err := appcore.RunDoctorChecks(ctx, appcore.DefaultDoctorChecks())
			if err != nil {
				return err
			}
			ok := appcore.PrintDoctorResults(cmd.OutOrStdout(), results)
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "\nRun 'blades init' to create missing files.")
				return errors.New("doctor found issues")
			}
			return nil
		},
	}
}
